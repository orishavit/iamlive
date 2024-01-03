package mapperclient

import (
	"context"
	"github.com/Khan/genqlient/graphql"
	"net/http"
)

type Client struct {
	address string
	client  graphql.Client
}

func NewClient(address string) *Client {
	client := *http.DefaultClient
	return &Client{
		address: address,
		client:  graphql.NewClient(address+"/query", &client),
	}
}

func (c *Client) ReportAWSOperation(ctx context.Context, operation []AWSOperation) error {
	_, err := reportAWSOperation(ctx, c.client, operation)
	return err
}
