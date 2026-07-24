# Build stage: compile both binaries for Lambda's arm64 (Graviton) fleet.
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ENV CGO_ENABLED=0 GOOS=linux GOARCH=arm64
RUN go build -tags lambda.norpc -o /out/bootstrap ./cmd/auto-lambda && \
    go build -o /out/bin/digest-mcp ./cmd/digest-mcp

# Runtime stage: AWS's custom-runtime base. Its entrypoint runs
# /var/runtime/bootstrap with cwd /var/task, so copying the repo layout into
# /var/task preserves the two cwd-relative contracts (projects/<name>,
# bin/digest-mcp) unchanged.
FROM public.ecr.aws/lambda/provided:al2023
COPY --from=build /out/bootstrap /var/runtime/bootstrap
COPY --from=build /out/bin/digest-mcp /var/task/bin/digest-mcp
COPY projects/ /var/task/projects/
CMD ["bootstrap"]