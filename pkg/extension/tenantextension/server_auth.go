package tenantauth

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/golang-jwt/jwt/v4"
	"go.opentelemetry.io/collector/client"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/extension"
	extensionauth "go.opentelemetry.io/collector/extension/auth"
	"go.uber.org/zap"
	"google.golang.org/grpc/metadata"
)

var (
	_ extension.Extension  = (*BearerTokenAuth)(nil)
	_ extensionauth.Server = (*BearerTokenAuth)(nil)
	_ extensionauth.Client = (*BearerTokenAuth)(nil)
)

// BearerTokenAuth is an implementation of extensionauth interfaces. It embeds a static authorization "bearer" token in every rpc call.
type BearerTokenAuth struct {
	header string
	scheme string

	shutdownCH chan struct{}

	commonKey *rsa.PublicKey

	jwksMap map[string]key
	mux     sync.RWMutex

	jwtCache sync.Map

	cfg    *Config
	logger *zap.Logger
}

func newBearerTokenAuth(cfg *Config, logger *zap.Logger) *BearerTokenAuth {
	a := &BearerTokenAuth{
		header: cfg.Header,
		scheme: cfg.Scheme,
		cfg:    cfg,
		logger: logger,
	}
	switch {
	case len(cfg.PublicKeyEnv) > 0:
		// TODO read public key from env
	case len(cfg.PublicKeyFile) > 0:
		// TODO read public key from file
	case len(cfg.PublicKey) > 0:
		decoded, err := base64.StdEncoding.DecodeString(cfg.PublicKey)
		if err != nil {
			logger.Fatal("failed to decode public key", zap.Error(err))
		}
		block, _ := pem.Decode(decoded)
		if block == nil || block.Type != "PUBLIC KEY" {
			logger.Fatal("failed to decode public key", zap.Error(err))
		}
		pubKey, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			logger.Fatal("failed to decode public key", zap.Error(err))
		}
		var ok bool
		a.commonKey, ok = pubKey.(*rsa.PublicKey)
		if !ok {
			logger.Fatal("pubKey is not RSA public key", zap.Error(err))
		}
	case len(cfg.JWKsURI) > 0:
		a.refreshPublicKey() // Load tokens from file
	}
	return a
}

// Start of BearerTokenAuth does nothing and returns nil if no filename
// is specified. Otherwise a routine is started to monitor the file containing
// the token to be transferred.
func (b *BearerTokenAuth) Start(ctx context.Context, _ component.Host) error {
	if len(b.cfg.JWKsURI) == 0 {
		// No need for watch jwks
		return nil
	}

	if b.shutdownCH != nil {
		return errors.New("bearerToken file monitoring is already running")
	}

	_ = b.refreshPublicKey()

	b.shutdownCH = make(chan struct{})
	go b.watchJWKs(ctx, b.shutdownCH)

	return nil
}

// if kid is not specified, will get the first public key
func (b *BearerTokenAuth) refreshPublicKey() error {
	if len(b.cfg.JWKsURI) == 0 {
		return errors.New("jwks uri is empty")
	}

	resp, err := http.Get(b.cfg.JWKsURI)
	if err != nil {
		return err
	}

	defer resp.Body.Close()
	body := []byte{}
	_, err = resp.Body.Read(body)
	if err != nil {
		return err
	}

	var jwks jwksResp
	err = json.Unmarshal(body, &jwks)
	if err != nil {
		return err
	}

	b.mux.Lock()
	defer b.mux.Unlock()
	for i := 0; i < len(jwks.Keys); i++ {
		key := jwks.Keys[i]
		if v, find := b.jwksMap[key.Kid]; find {
			if v.N[:12] == key.N[:12] {
				continue
			}
		}
		publicKey, err := decodePublicKey(key.N, key.E)
		if err != nil {
			return err
		}
		key.publicKey = publicKey
		b.jwksMap[key.Kid] = key
	}
	return nil
}

func (b *BearerTokenAuth) watchJWKs(ctx context.Context, stopCH <-chan struct{}) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stopCH:
			return
		case <-ticker.C:
			err := b.refreshPublicKey()
			if err != nil {
				b.logger.Error("update jwks failed", zap.Error(err))
			}
		}
	}
}

type jwksResp struct {
	Keys []key `json:"keys"`
}

type key struct {
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`

	publicKey *rsa.PublicKey `json:"-"`
}

// Shutdown of BearerTokenAuth does nothing and returns nil
func (b *BearerTokenAuth) Shutdown(_ context.Context) error {
	if b.shutdownCH == nil {
		return nil
	}
	b.shutdownCH <- struct{}{}
	close(b.shutdownCH)
	b.shutdownCH = nil
	return nil
}

// RoundTripper is not implemented by BearerTokenAuth
func (b *BearerTokenAuth) RoundTripper(base http.RoundTripper) (http.RoundTripper, error) {
	return &BearerAuthRoundTripper{
		header: b.header,
		base:   base,
	}, nil
}

// Authenticate checks whether the given context contains valid auth data. Validates tokens from clients trying to access the service (incoming requests)
func (b *BearerTokenAuth) Authenticate(ctx context.Context, headers map[string][]string) (context.Context, error) {
	auth, ok := headers[strings.ToLower(b.header)]
	if !ok {
		auth, ok = headers[b.header]
	}
	if !ok || len(auth) == 0 {
		return ctx, fmt.Errorf("missing or empty authorization header: %s", b.header)
	}

	token, err := b.getUserInfoFromJWT(auth[0])
	if err != nil {
		return ctx, err
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok {
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			md = metadata.New(nil)
		} else {
			md = md.Copy()
		}

		if tenant, ok := claims["tenant"].(map[string]any); ok {
			if tenantID, ok := tenant["tenant_id"].(string); ok {
				md.Set("tenant_id", tenantID)
			}
			if tenantName, ok := tenant["tenant_name"].(string); ok {
				md.Set("tenant_name", tenantName)
			}
			if account_id, ok := tenant["account_id"].(float64); ok {
				md.Set("account_id", strconv.FormatInt(int64(account_id), 10))
			}
		}

		ctx = client.NewContext(ctx, client.Info{
			Addr:     nil,
			Metadata: client.NewMetadata(md),
		})
	}
	return ctx, nil
}

func decodePublicKey(nB64, eB64 string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nB64)
	if err != nil {
		return nil, fmt.Errorf("invalid n: %w", err)
	}

	eBytes, err := base64.RawURLEncoding.DecodeString(eB64)
	if err != nil {
		return nil, fmt.Errorf("invalid e: %w", err)
	}

	var eInt int
	for _, b := range eBytes {
		eInt = eInt<<8 + int(b)
	}

	pubKey := &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: eInt,
	}
	return pubKey, nil
}

func (b *BearerTokenAuth) getUserInfoFromJWT(tokenStr string) (*jwt.Token, error) {
	hash := xxhash.Sum64String(tokenStr)
	if val, ok := b.jwtCache.Load(hash); ok {
		if info, ok := val.(*jwt.Token); ok {
			// TODO check expired
			return info, nil
		}
	}

	parts := strings.SplitN(tokenStr, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return nil, errors.New("invalid authorization header format")
	}

	userInfo := parts[1]

	b.mux.RLock()
	defer b.mux.RUnlock()
	token, err := jwt.Parse(userInfo, func(token *jwt.Token) (interface{}, error) {
		if token.Method.Alg() != jwt.SigningMethodRS256.Alg() {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		if b.jwksMap != nil {
			if v, find := b.jwksMap[token.Header["kid"].(string)]; find {
				return v.publicKey, nil
			}
		}
		if b.commonKey != nil {
			return b.commonKey, nil
		}
		return nil, errors.New("no public key")
	}, jwt.WithValidMethods([]string{"RS256"}))

	if err != nil || !token.Valid {
		return nil, fmt.Errorf("invalid JWT: %w", err)
	}

	b.jwtCache.Store(hash, token)
	return token, nil
}
