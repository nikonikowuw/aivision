// Package main 实现用于端到端测试直接调用 Engine UDS gRPC 接口的命令行客户端工具
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"


	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protojson"

	pb "stub_server/gen/aivision/v1"
)

func main() {
	engineSock := flag.String("engine-sock", "/tmp/aivision_engine_test.sock", "Engine UDS socket path")
	cmd := flag.String("cmd", "", "Command to execute: query_profile, apply_desired_state, install_package, query_metrics, delete_images, probe_camera")
	payloadFile := flag.String("payload", "", "JSON payload file path")
	timeoutSec := flag.Int("timeout", 5, "RPC deadline in seconds")
	flag.Parse()

	if *cmd == "" {
		fmt.Println("Usage: client -engine-sock <path> -cmd <command> [-payload <file.json>]")
		os.Exit(1)
	}

	conn, err := grpc.Dial("unix://"+*engineSock, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		fmt.Printf("Failed to connect to engine: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	client := pb.NewEngineServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeoutSec)*time.Second)
	defer cancel()

	switch *cmd {
	case "query_profile":
		resp, err := client.QueryProfile(ctx, &pb.QueryProfileRequest{})
		if err != nil {
			fmt.Printf("RPC error: %v\n", err)
			os.Exit(1)
		}
		out, _ := protojson.Marshal(resp)
		fmt.Println(string(out))

	case "query_metrics":
		resp, err := client.QueryMetrics(ctx, &pb.QueryMetricsRequest{})
		if err != nil {
			fmt.Printf("RPC error: %v\n", err)
			os.Exit(1)
		}
		out, _ := protojson.Marshal(resp)
		fmt.Println(string(out))

	case "apply_desired_state":
		if *payloadFile == "" {
			fmt.Println("-payload required for apply_desired_state")
			os.Exit(1)
		}
		data, err := os.ReadFile(*payloadFile)
		if err != nil {
			fmt.Printf("Failed to read payload: %v\n", err)
			os.Exit(1)
		}
		var ds pb.DesiredState
		if err := protojson.Unmarshal(data, &ds); err != nil {
			fmt.Printf("Failed to unmarshal desired state: %v\n", err)
			os.Exit(1)
		}
		resp, err := client.ApplyDesiredState(ctx, &pb.ApplyDesiredStateRequest{DesiredState: &ds})
		if err != nil {
			fmt.Printf("RPC error: %v\n", err)
			os.Exit(1)
		}
		out, _ := protojson.Marshal(resp)
		fmt.Println(string(out))

	case "install_package":
		if *payloadFile == "" {
			fmt.Println("-payload required for install_package")
			os.Exit(1)
		}
		data, err := os.ReadFile(*payloadFile)
		if err != nil {
			fmt.Printf("Failed to read payload: %v\n", err)
			os.Exit(1)
		}
		var req pb.InstallPackageRequest
		if err := protojson.Unmarshal(data, &req); err != nil {
			fmt.Printf("Failed to unmarshal install_package: %v\n", err)
			os.Exit(1)
		}
		resp, err := client.InstallPackage(ctx, &req)
		if err != nil {
			fmt.Printf("RPC error: %v\n", err)
			os.Exit(1)
		}
		out, _ := protojson.Marshal(resp)
		fmt.Println(string(out))

	case "delete_images":
		if *payloadFile == "" {
			fmt.Println("-payload required for delete_images")
			os.Exit(1)
		}
		data, err := os.ReadFile(*payloadFile)
		if err != nil {
			fmt.Printf("Failed to read payload: %v\n", err)
			os.Exit(1)
		}
		var req pb.DeleteImagesRequest
		if err := json.Unmarshal(data, &req); err != nil {
			fmt.Printf("Failed to unmarshal delete_images: %v\n", err)
			os.Exit(1)
		}
		resp, err := client.DeleteImages(ctx, &req)
		if err != nil {
			fmt.Printf("RPC error: %v\n", err)
			os.Exit(1)
		}
		out, _ := protojson.Marshal(resp)
		fmt.Println(string(out))

	case "probe_camera":
		if *payloadFile == "" {
			fmt.Println("-payload required for probe_camera")
			os.Exit(1)
		}
		data, err := os.ReadFile(*payloadFile)
		if err != nil {
			fmt.Printf("Failed to read payload: %v\n", err)
			os.Exit(1)
		}
		var req pb.ProbeCameraRequest
		if err := protojson.Unmarshal(data, &req); err != nil {
			fmt.Printf("Failed to unmarshal probe_camera: %v\n", err)
			os.Exit(1)
		}
		resp, err := client.ProbeCamera(ctx, &req)
		if err != nil {
			fmt.Printf("RPC error: %v\n", err)
			os.Exit(1)
		}
		out, _ := protojson.Marshal(resp)
		fmt.Println(string(out))

	default:
		fmt.Printf("Unknown command: %s\n", *cmd)
		os.Exit(1)
	}
}
