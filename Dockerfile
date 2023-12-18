FROM golang:1.19-alpine as builder
RUN apk add --no-cache ca-certificates git protoc
RUN apk add build-base

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.sum ./

RUN go mod download

COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -a -o iamlive .

FROM gcr.io/distroless/static:nonroot
WORKDIR /otterize/iamlive
COPY --from=builder /workspace/iamlive .

EXPOSE 10080
ENTRYPOINT ["/otterize/iamlive/iamlive"]
