package jaeger

import (
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"go.uber.org/zap"
)

const (
	JSONEncoding     string = "json"
	ProtobufEncoding string = "protobuf"
)

//go:embed sqlscripts/*
var SQLScripts embed.FS

func WalkMatch(root, pattern string) ([]string, error) {
	var matches []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if matched, err := filepath.Match(pattern, filepath.Base(path)); err != nil {
			return err
		} else if matched {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return matches, nil
}

func Render(templates *template.Template, filename string, args interface{}) string {
	var statement strings.Builder
	err := templates.ExecuteTemplate(&statement, filename, args)
	if err != nil {
		panic(err)
	}
	return statement.String()
}

func ExecuteScripts(logger *zap.Logger, sqlStatements []string, db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	for _, statement := range sqlStatements {
		logger.Debug("Running SQL statement", zap.Any("statement", statement))
		_, err = tx.Exec(statement)
		if err != nil {
			return fmt.Errorf("could not run sql %q: %q", statement, err)
		}
	}
	committed = true
	return tx.Commit()
}
