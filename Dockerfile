FROM golang:1.22-alpine AS builder
WORKDIR /workspace

RUN apk add --no-cache git

COPY go.mod ./
COPY cmd/ cmd/
COPY internal/ internal/

RUN go mod tidy && go mod download

RUN CGO_ENABLED=0 GOOS=linux go build -a -o operator ./cmd/operator/main.go

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/operator .
USER 65532:65532
ENTRYPOINT ["/operator"]