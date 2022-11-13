# `secretsctx`

`secretsctx` is a Lambda extension (packaged as a Lambda layer) that injects 
secret values from AWS Parameter Store and AWS Secrets Manager into your Lambda
function's invocation "context". Here's how it works:

```yaml
Transform: AWS::Serverless-2016-10-31

Resources:
  NodeExample:
    Type: AWS::Serverless::Function
    Properties:
      Runtime: nodejs16.x
      Architectures: [arm64]
      Handler: index.handler
      Layers: [!Ref SecretsCtxLayer]
      Environment:
        Variables:
          SECRETSCTXFREQUENCY: "1m"
          SECRETSCTX_username: "{aws-ssm}/my-app/username"
          SECRETSCTX_password: "{aws-ssm}/my-app/db-password"
      Policies:
        - SSMParameterReadPolicy:
            ParameterName: my-app/*
      InlineCode: |
        exports.handler = async function(event, context) {
          const { username, password } = context.clientContext.env;
          return { username, password };
        }
```

In this example, the layer works as follows:

* Looks for environment variables of the pattern `SECRETSCTX_${name}`.
* Fetches the values for those env vars using `ssm:GetParameters` and 
  `secretsmanager:GetSecretValue`.
* Populates the function handler context object. The above example is for 
  Node.js, but it works equally well in Python or any other runtime.
* An optional `SECRETSCTXFREQUENCY` env var tells the extension to refresh
  the values on a schedule, not just on a function cold start. This happens
  **after** your function has returned, so it incurs no latency penalty.

The end result:

![example](/docs/example.png)

## How it works

The short version: the extension proxies the Lambda runtime API and modifies
the `Lambda-Runtime-Client-Context` response header returned by `/runtime/invocation/next`.
It modifies the `AWS_LAMBDA_RUNTIME_API` environment variable to be `127.0.0.1:8088`
(the proxy's port) instead of `127.0.0.1:9001` (the runtime's real port). 

The marginally longer version: a sensible person would modify `AWS_LAMBDA_RUNTIME_API`
before executing the function runtime using a [wrapper script][wrapper-script]. 
But I recently learned about the insanely cool [`lambda-spy`][lambda-spy] and 
shamelessly repurposed their code for a "valid" use case (for a very generous 
definition of valid).

See also my sister project [`cloudenv`][cloudenv] - which is how I personally 
prefer to handle secrets. I wrote this as a demo to prove that it could work via
invocation context, as some other folks are quite passionate about that.

[wrapper-script]: https://docs.aws.amazon.com/lambda/latest/dg/runtimes-modify.html#runtime-wrapper
[lambda-spy]: https://www.clearvector.com/blog/lambda-spy/
[cloudenv]: https://github.com/aidansteele/cloudenv
