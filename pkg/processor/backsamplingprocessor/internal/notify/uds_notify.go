package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/CloudDetail/apo-module/model/v1"
	"github.com/gin-gonic/gin"
)

const (
	sockAddr       = "/opt/signal/uds-register.sock"
	RegistryURL    = "/register"
	HealthCheckURL = "/already-register-healthcheck"
)

type Notify interface {
	SendSignal(trace *model.TraceLabels)
	BatchSendSignals()
}

type NoopNotify struct{}

func (notify *NoopNotify) SendSignal(trace *model.TraceLabels) {}

func (notify *NoopNotify) BatchSendSignals() {}

type UdsRegistryServer struct {
	lock   sync.Mutex
	engine *gin.Engine

	signals    []*Signal
	udsClients []*UdsClient
	logEnable  bool
}

func NewUdsRegistryServer(logEnable bool) (*UdsRegistryServer, error) {
	dir, _ := filepath.Split(sockAddr)
	if sock, err := os.Stat(sockAddr); err == nil {
		if !sock.IsDir() {
			err := os.Remove(sockAddr)
			if err != nil {
				return nil, fmt.Errorf("file[%s] exist , remove failed, err: %v", sockAddr, err)
			}
		} else {
			return nil, fmt.Errorf("filePath file[%s] cannot create, exist dir", sockAddr)
		}
	} else if _, err := os.Stat(dir); os.IsNotExist(err) {
		err := os.MkdirAll(dir, os.ModePerm)
		if err != nil {
			return nil, fmt.Errorf("parentPath dir[%s] cannot create, err: %v", dir, err)
		}
	}
	gin.SetMode(gin.ReleaseMode)
	server := &UdsRegistryServer{
		engine:     gin.New(),
		udsClients: make([]*UdsClient, 0),
		signals:    make([]*Signal, 0),
		logEnable:  logEnable,
	}
	server.engine.POST(RegistryURL, server.registry)
	server.engine.POST(HealthCheckURL, server.healthCheck)

	unixAddr, err := net.ResolveUnixAddr("unix", sockAddr)
	if err != nil {
		return nil, err
	}
	listener, err := net.ListenUnix("unix", unixAddr)
	if err != nil {
		return nil, err
	}

	go http.Serve(listener, server.engine)

	return server, nil
}

func (server *UdsRegistryServer) registry(c *gin.Context) {
	registryArg := RegisterArg{}
	if err := c.BindJSON(&registryArg); err != nil {
		log.Printf("Error deserializing registerArgs: %s", err.Error())
		c.String(200, "register success")
		return
	}

	if server.logEnable {
		log.Print("Received UDS RegisterArg")
	}
	for _, udsClient := range server.udsClients {
		if udsClient.component == registryArg.Component {
			udsClient.isHealth = true
			c.String(200, "register success")
			return
		}
	}

	udsClient := buildUdsClient(registryArg.SockAddr, registryArg.UrlPath, registryArg.Component)
	server.udsClients = append(server.udsClients, udsClient)
	c.String(200, "register success")
}

func (server *UdsRegistryServer) healthCheck(c *gin.Context) {
	alreadyRegistryArg := AlreadyRegisterArg{}
	if err := c.BindJSON(&alreadyRegistryArg); err != nil {
		c.String(200, "error, cannot parse request argument")
		return
	}

	if server.logEnable {
		log.Print("Receive health check")
	}
	for _, udsClient := range server.udsClients {
		if udsClient.component == alreadyRegistryArg.Component {
			if udsClient.isHealth {
				// If the component already exists and health, return "ok"
				c.String(200, "ok")
				return
			} else {
				c.String(200, "error, already register but not health")
				return
			}
		}
	}

	c.String(200, "error, not exist")
}

func (server *UdsRegistryServer) SendSignal(trace *model.TraceLabels) {
	server.lock.Lock()
	defer server.lock.Unlock()
	server.signals = append(server.signals, buildSignal(trace))
}

func (server *UdsRegistryServer) BatchSendSignals() {
	signalCount := len(server.signals)
	if signalCount == 0 {
		return
	}
	server.lock.Lock()
	signals := server.signals[0:]
	server.signals = server.signals[signalCount:]
	server.lock.Unlock()

	for _, notify := range server.udsClients {
		client := &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					return net.Dial("unix", notify.sockAddr)
				},
			},
			Timeout: time.Second * 10,
		}
		for _, signal := range signals {
			if !notify.isHealth {
				continue
			}
			body, _ := json.Marshal(signal)
			_, err := client.Post(fmt.Sprintf("http://localhost%s", notify.urlPath), "application/json", bytes.NewReader(body))
			if err != nil {
				log.Printf("%s network Error %s. will remove from notifies_to_send", notify.component, err.Error())
				notify.isHealth = false
			}
		}
	}
}

type UdsClient struct {
	client    *http.Client
	sockAddr  string
	urlPath   string
	component string
	isHealth  bool
}

func buildUdsClient(sockAddr string, urlPath string, component string) *UdsClient {
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return net.Dial("unix", sockAddr)
			},
		},
		Timeout: time.Second * 10,
	}
	return &UdsClient{
		client:    client,
		sockAddr:  sockAddr,
		urlPath:   urlPath,
		component: component,
		isHealth:  true,
	}
}

type RegisterArg struct {
	Component string `json:"component"`
	SockAddr  string `json:"sock_addr,omitempty"`
	UrlPath   string `json:"url_path,omitempty"`
}

type AlreadyRegisterArg struct {
	Component string `json:"component"`
}

type Result struct {
	Code    int    `json:"code"`
	Message string `json:"msg"`
}
