FROM golang:1.23.4-alpine as builder

WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -a -o iamlive .

FROM gcr.io/distroless/static:nonroot
WORKDIR /otterize/iamlive
COPY --from=builder /workspace/iamlive .

EXPOSE 10080
ENTRYPOINT ["/otterize/iamlive/iamlive"]
