FROM golang:1.24 AS builder
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/webhook-sidecard

FROM gcr.io/distroless/base-debian12
COPY --from=builder /out/webhook-sidecard /webhook-sidecard
USER 65532:65532
ENTRYPOINT ["/webhook-sidecard"]
