package main

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"golang.org/x/sync/errgroup"
	"os"
	"strings"
	"time"
)

const prefixSsm = "{aws-ssm}"
const prefixSm = "{aws-sm}"
const envPrefix = "SECRETSCTX_"

type resolvedValue struct {
	source string
	value  string
}

func (s *spyext) fetch(ctx context.Context) error {
	params := map[string]*resolvedValue{}
	secrets := map[string]*resolvedValue{}

	for _, kv := range os.Environ() {
		name, value, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, envPrefix) {
			key := strings.TrimPrefix(name, envPrefix)
			if strings.HasPrefix(value, prefixSm) {
				secrets[key] = &resolvedValue{source: strings.TrimPrefix(value, prefixSm)}
			} else if strings.HasPrefix(value, prefixSsm) {
				params[key] = &resolvedValue{source: strings.TrimPrefix(value, prefixSsm)}
			} else {
				panic(fmt.Sprintf("only {aws-sm} and {aws-ssm} supported but you provided %s=%s", name, value))
			}
		}
	}

	// populate map with values from parameter store
	err := populateParameters(ctx, s.ssm, params)
	if err != nil {
		panic(fmt.Sprintf("%+v", err))
	}

	// populate map with values from secrets manager
	err = populateSecrets(ctx, s.sm, secrets, 5)
	if err != nil {
		panic(fmt.Sprintf("%+v", err))
	}

	s.env = map[string]string{}
	for k, v := range params {
		s.env[k] = v.value
	}
	for k, v := range secrets {
		s.env[k] = v.value
	}

	s.lastFetched = time.Now()
	return nil
}

func populateParameters(ctx context.Context, api *ssm.Client, params map[string]*resolvedValue) error {
	nameMap := map[string]string{}
	for _, v := range params {
		nameMap[v.source] = ""
	}

	nameChunks := chunk[string](keys(nameMap), 10)
	for _, names := range nameChunks {
		get, err := api.GetParameters(ctx, &ssm.GetParametersInput{
			Names:          names,
			WithDecryption: aws.Bool(true),
		})
		if err != nil {
			return fmt.Errorf("getting parameters: %w", err)
		}

		if len(get.InvalidParameters) > 0 {
			return fmt.Errorf("invalid parameters: %s", strings.Join(get.InvalidParameters, ", "))
		}

		for _, parameter := range get.Parameters {
			nameMap[*parameter.Name] = *parameter.Value
		}
	}

	for k, v := range params {
		params[k].value = nameMap[v.source]
	}

	return nil
}

type resolvedSecret struct {
	arn   string
	value string
}

func populateSecrets(ctx context.Context, api *secretsmanager.Client, secrets map[string]*resolvedValue, concurrency int) error {
	inch := make(chan string, len(secrets))
	for _, v := range secrets {
		inch <- v.source
	}
	close(inch)

	outch := make(chan resolvedSecret, len(secrets))

	g, ctx := errgroup.WithContext(ctx)
	for idx := 0; idx < concurrency; idx++ {
		g.Go(func() error {
			for secret := range inch {
				gsv, err := api.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
					SecretId: &secret,
				})
				if err != nil {
					return fmt.Errorf(": %w", err)
				}

				if gsv.SecretString != nil {
					outch <- resolvedSecret{arn: secret, value: *gsv.SecretString}
				} else if gsv.SecretBinary != nil {
					outch <- resolvedSecret{arn: secret, value: string(gsv.SecretBinary)}
				}
			}

			return nil
		})
	}

	err := g.Wait()
	if err != nil {
		return fmt.Errorf("getting secrets: %w", err)
	}

	for idx := 0; idx < len(secrets); idx++ {
		secret := <-outch

		for _, v := range secrets {
			if v.source == secret.arn {
				v.value = secret.value
			}
		}
	}

	close(outch)
	return nil
}

func keys[K comparable, V any](m map[K]V) []K {
	slice := make([]K, len(m))

	idx := 0
	for t := range m {
		slice[idx] = t
		idx++
	}

	return slice
}

func chunk[T any](slice []T, chunkSize int) [][]T {
	var chunks [][]T

	for i := 0; i < len(slice); i += chunkSize {
		end := i + chunkSize

		// necessary check to avoid slicing beyond
		// slice capacity
		if end > len(slice) {
			end = len(slice)
		}

		chunks = append(chunks, slice[i:end])
	}

	return chunks
}
