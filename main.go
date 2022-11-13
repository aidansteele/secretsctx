package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/aidansteele/secretsctx/extension"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

func main() {
	var err error
	ctx := context.Background()

	runtimeAddr := os.Getenv("AWS_LAMBDA_RUNTIME_API")
	proxyAddr := "127.0.0.1:8088"
	err = patchRapid([]byte(runtimeAddr), []byte(proxyAddr))
	if err != nil {
		panic(fmt.Sprintf("%+v", err))
	}

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		panic(fmt.Sprintf("%+v", err))
	}

	var frequency time.Duration
	if freqstr := os.Getenv("SECRETSCTXFREQUENCY"); freqstr != "" {
		frequency, err = time.ParseDuration(freqstr)
		if err != nil {
			panic(fmt.Sprintf("invalid string trying to parse refresh frequency %+v", err))
		}
	}

	s := &spyext{
		runtimeAddr: runtimeAddr,
		proxyAddr:   proxyAddr,
		funcDone:    make(chan struct{}),
		sm:          secretsmanager.NewFromConfig(cfg),
		ssm:         ssm.NewFromConfig(cfg),
		frequency:   frequency,
	}

	go s.serveProxy()

	err = s.fetch(ctx)
	if err != nil {
		panic(fmt.Sprintf("%+v", err))
	}

	s.extension = extension.NewClient(runtimeAddr)
	_, err = s.extension.Register(ctx, filepath.Base(os.Args[0]))
	if err != nil {
		panic(fmt.Sprintf("%+v", err))
	}

	err = s.serveExtension(ctx)
	if err != nil {
		panic(fmt.Sprintf("%+v", err))
	}
}

type spyext struct {
	runtimeAddr string
	proxyAddr   string
	funcDone    chan struct{}
	extension   *extension.Client
	sm          *secretsmanager.Client
	ssm         *ssm.Client
	env         map[string]string
	lastFetched time.Time
	frequency   time.Duration
}

func (s *spyext) serveExtension(ctx context.Context) error {
	for {
		_, err := s.extension.NextEvent(ctx)
		if err != nil {
			return fmt.Errorf(": %w", err)
		}

		// we wait til function is done before refreshing secrets, don't want to compete for cpu/io
		<-s.funcDone
		if s.frequency > 0 && time.Now().Sub(s.lastFetched) > s.frequency {
			err = s.fetch(ctx)
			if err != nil {
				panic(fmt.Sprintf("%+v", err))
			}
		}
	}
}

func (s *spyext) serveProxy() {
	u, _ := url.Parse("http://" + s.runtimeAddr)
	rev := httputil.NewSingleHostReverseProxy(u)

	re := regexp.MustCompile(`/2018-06-01/runtime/invocation/[^/]+/(error|response)`)

	rev.ModifyResponse = func(response *http.Response) error {
		path := response.Request.URL.Path
		if re.MatchString(path) {
			s.funcDone <- struct{}{}
		}

		if path != "/2018-06-01/runtime/invocation/next" {
			return nil
		}

		cc, _ := json.Marshal(map[string]any{"env": s.env})
		response.Header.Set("Lambda-Runtime-Client-Context", string(cc))
		return nil
	}

	err := http.ListenAndServe(s.proxyAddr, rev)
	if err != nil {
		panic(fmt.Sprintf("%+v", err))
	}
}
