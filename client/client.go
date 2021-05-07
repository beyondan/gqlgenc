package client

import (
	"context"
	"fmt"
	"github.com/infiotinc/gqlgenc/client/transport"
)

type Client struct {
	Transport transport.Transport
}

func (c *Client) do(ctx context.Context, operation transport.Operation, operationName string, query string, variables map[string]interface{}, t interface{}) error {
	res, err := c.Transport.Request(transport.Request{
		Context:       ctx,
		Operation:     operation,
		Query:         query,
		OperationName: operationName,
		Variables:     variables,
	})
	if err != nil {
		return err
	}

	go func() {
		<-ctx.Done()
		res.Close()
	}()

	ok := res.Next()
	if !ok {
		return fmt.Errorf("no response")
	}

	if err := res.Err(); err != nil {
		return err
	}

	opres := res.Get()
	err = opres.UnmarshalData(t)

	if len(opres.Errors) > 0 {
		return opres.Errors
	}

	return err
}

// Query runs a query
// operationName is optional
func (c *Client) Query(ctx context.Context, operationName string, query string, variables map[string]interface{}, t interface{}) error {
	return c.do(ctx, transport.Query, operationName, query, variables, t)
}

// Mutation runs a mutation
// operationName is optional
func (c *Client) Mutation(ctx context.Context, operationName string, query string, variables map[string]interface{}, t interface{}) error {
	return c.do(ctx, transport.Mutation, operationName, query, variables, t)
}

// Subscription starts a GQL subscription
// operationName is optional
func (c *Client) Subscription(ctx context.Context, operationName string, query string, variables map[string]interface{}) (transport.Response, error) {
	res, err := c.Transport.Request(transport.Request{
		Context:       ctx,
		Operation:     transport.Subscription,
		Query:         query,
		OperationName: operationName,
		Variables:     variables,
	})
	if err != nil {
		return nil, err
	}

	go func() {
		<-ctx.Done()
		res.Close()
	}()

	return res, nil
}
