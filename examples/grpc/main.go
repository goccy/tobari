package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"

	"github.com/goccy/tobari"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"examples/grpc/pb"
)

type server struct {
	examplepb.UnimplementedExampleServer
}

func (s *server) Echo(_ context.Context, req *examplepb.EchoRequest) (*examplepb.EchoResponse, error) {
	return &examplepb.EchoResponse{
		Message: req.GetMessage(),
	}, nil
}

const bufSize = 1024

var (
	listener *bufconn.Listener
)

func dialer(ctx context.Context, address string) (net.Conn, error) {
	return listener.Dial()
}

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func tobariInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	var (
		res any
		err error
	)
	tobari.Cover(func() {
		res, err = handler(ctx, req)
	})
	return res, err
}

func run(ctx context.Context) error {
	listener = bufconn.Listen(bufSize)

	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(tobariInterceptor),
	)
	defer grpcServer.Stop()

	examplepb.RegisterExampleServer(grpcServer, new(server))

	conn, err := grpc.DialContext(
		ctx, "",
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return err
	}
	defer conn.Close()

	go func() {
		if err := grpcServer.Serve(listener); err != nil {
			panic(err)
		}
	}()

	client := examplepb.NewExampleClient(conn)
	res, err := client.Echo(ctx, &examplepb.EchoRequest{
		Message: "hello",
	})
	if err != nil {
		return err
	}
	fmt.Println(res.GetMessage())

	f, err := os.Create("out.cover")
	if err != nil {
		return err
	}
	tobari.WriteCoverprofile(tobari.SetMode, f)
	return nil
}
