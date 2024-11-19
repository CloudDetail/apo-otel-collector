package parser

import (
	"fmt"
	"strings"
)

type HttpParser struct {
	urlParser string
}

func NewHttpParser(urlParser string) *HttpParser {
	return &HttpParser{
		urlParser: urlParser,
	}
}

func (parser *HttpParser) Parse(httpMethod string, url string) string {
	if parser.urlParser == "topUrl" {
		return fmt.Sprintf("%s %s", httpMethod, getTopUrl(url))
	}
	return httpMethod
}

func getTopUrl(url string) string {
	if url == "" {
		return "unknown"
	}
	urls := strings.Split(url, "/")
	if len(urls) == 1 {
		return url
	}
	if len(urls) >= 3 && strings.HasSuffix(urls[0], ":") && urls[1] == "" {
		// schema://host:port/path
		if len(urls) == 3 {
			return "/"
		} else {
			return fmt.Sprintf("/%s", urls[3])
		}
	}
	return fmt.Sprintf("/%s", urls[1])
}
