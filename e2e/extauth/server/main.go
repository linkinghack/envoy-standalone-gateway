package main

import (
	"context"
	"log"
	"net"
	"net/http"

	authv3 "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
	"google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

type authorizationServer struct {
	authv3.UnimplementedAuthorizationServer
}

func (authorizationServer) Check(_ context.Context, req *authv3.CheckRequest) (*authv3.CheckResponse, error) {
	code := codes.PermissionDenied
	if req.GetAttributes().GetRequest().GetHttp().GetHeaders()["authorization"] == "allow" {
		code = codes.OK
	}
	return &authv3.CheckResponse{Status: &status.Status{Code: int32(code)}}, nil
}

func main() {
	grpcListener, err := net.Listen("tcp", ":19001")
	if err != nil {
		log.Fatal(err)
	}
	grpcServer := grpc.NewServer()
	authv3.RegisterAuthorizationServer(grpcServer, authorizationServer{})
	go func() {
		if err := grpcServer.Serve(grpcListener); err != nil {
			log.Fatal(err)
		}
	}()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "allow" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "denied", http.StatusForbidden)
	})
	log.Fatal(http.ListenAndServe(":19000", handler))
}
