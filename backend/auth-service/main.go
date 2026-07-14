package main

import (
	pb "auth-service/authpb"
	"context"
	"errors"
	"fmt"
	"log"
	"net"

	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AuthServer struct {
	pb.UnimplementedAuthServiceServer
}

var rdb *redis.Client

func (a *AuthServer) VerifyUser(ctx context.Context, in *pb.Input) (*pb.Output, error) {
	var output bool
	login := in.GetLogin()
	password := in.GetPassword()

	// 1. Get password from Redis with .Result() method

	res, err := rdb.Get(ctx, login).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, status.Errorf(codes.NotFound, "Invalid login or password!")
		}
		return nil, status.Errorf(codes.Internal, "Unexpected error")
	}

	// 2. Use CompsreHashAndPassword() method from bcrypt

	isValid := bcrypt.CompareHashAndPassword([]byte(res), []byte(password))
	if isValid != nil {
		return nil, status.Errorf(codes.PermissionDenied, "Invalid login or password!")
	}

	// 3. Return result

	output = true

	return &pb.Output{Verification: output}, nil
}

func (a *AuthServer) UploadUser(ctx context.Context, in *pb.Input) (*pb.Resp, error) {

	login := in.GetLogin()
	password := in.GetPassword()

	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Println(err)
		return &pb.Resp{Result: false, Msg: "Hash generation"}, err
	}

	err = rdb.Set(ctx, login, string(bytes), 0).Err()
	if err != nil {
		log.Println(err)
		return &pb.Resp{Result: false, Msg: "Writing in Redis"}, err
	}

	log.Printf("User has been added! %s\n", login)

	return &pb.Resp{Result: true, Msg: login + " has been registered"}, nil
}

func (a *AuthServer) DeleteUser(ctx context.Context, in *pb.Input) (*pb.Resp, error) {
	login := in.GetLogin()

	_, err := a.VerifyUser(ctx, in)
	if err != nil {
		return nil, status.Errorf(codes.PermissionDenied, "Invalid login or password!")
	}

	_, err = rdb.Unlink(ctx, login).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, status.Errorf(codes.NotFound, "User not found: %s", login)
		}
		return nil, status.Errorf(codes.Internal, "Unexpected error!")
	}

	log.Printf("User has been deleted: %s\n", login)

	return &pb.Resp{Result: true, Msg: "User has been deleted successfully " + login}, nil
}

func InitRdb(addr string) *redis.Client {
	rdbTmp := redis.NewClient(&redis.Options{
		Password: "",
		Addr:     addr,
		DB:       1,
	})
	return rdbTmp
}

func main() {

	rdb = InitRdb("redis:6379")

	listener, err := net.Listen("tcp", ":11002")
	if err != nil {
		fmt.Printf("%v\n", err)
		return
	}
	defer listener.Close()

	grpcServer := grpc.NewServer()

	pb.RegisterAuthServiceServer(grpcServer, &AuthServer{})

	if err := grpcServer.Serve(listener); err != nil {
		fmt.Printf("ERR: %v", err)
		return
	}
}
