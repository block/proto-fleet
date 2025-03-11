package api

import (
	"connectrpc.com/connect"
	"context"
	"fmt"
	greetv1 "github.com/btc-mining/miner-firmware/fleet/api/gen/greet/v1"
	"github.com/btc-mining/miner-firmware/fleet/api/gen/greet/v1/greetv1connect"
	"log"
)

type GreetServer struct{}

var _ greetv1connect.GreetServiceHandler = &GreetServer{}

func (s *GreetServer) Greet(
	ctx context.Context,
	req *connect.Request[greetv1.GreetRequest],
) (*connect.Response[greetv1.GreetResponse], error) {
	log.Println("Request headers: ", req.Header())
	res := connect.NewResponse(&greetv1.GreetResponse{
		Greeting: fmt.Sprintf("Hello, %s!", req.Msg.Name),
	})
	res.Header().Set("Greet-Version", "v1")
	return res, nil
}
