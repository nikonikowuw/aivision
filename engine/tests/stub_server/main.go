// Package main 实现端到端集成测试用的 Go Stub 控制面服务
// 提供 HTTP REST 接口供测试脚本下发指令，提供 gRPC UDS 服务（ReportService / ControlPlaneService）供 Engine 连接
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"


	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"

	pb "stub_server/gen/aivision/v1"
)

type ControlServer struct {
	state      *ServerState
	httpServer *http.Server
	grpcServer *grpc.Server
	sockPath   string
	httpAddr   string
}

func NewControlServer(sockPath, httpAddr string, state *ServerState) *ControlServer {
	return &ControlServer{
		state:    state,
		sockPath: sockPath,
		httpAddr: httpAddr,
	}
}

func (cs *ControlServer) Start() error {
	_ = os.Remove(cs.sockPath)

	l, err := net.Listen("unix", cs.sockPath)
	if err != nil {
		return fmt.Errorf("failed to listen on unix socket %s: %w", cs.sockPath, err)
	}

	cs.grpcServer = grpc.NewServer()
	pb.RegisterControlPlaneServiceServer(cs.grpcServer, NewControlPlaneService(cs.state))
	pb.RegisterReportServiceServer(cs.grpcServer, NewReportService(cs.state))

	go func() {
		if err := cs.grpcServer.Serve(l); err != nil {
			fmt.Printf("gRPC server error: %v\n", err)
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/desired_state", cs.handleDesiredState)
	mux.HandleFunc("/alarms", cs.handleAlarms)
	mux.HandleFunc("/task_states", cs.handleTaskStates)
	mux.HandleFunc("/instance_states", cs.handleInstanceStates)
	mux.HandleFunc("/telemetry", cs.handleTelemetry)
	mux.HandleFunc("/orphan_reports", cs.handleOrphanReports)
	mux.HandleFunc("/orphan_policy", cs.handleOrphanPolicy)
	mux.HandleFunc("/fault_injection", cs.handleFaultInjection)
	mux.HandleFunc("/reset", cs.handleReset)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	cs.httpServer = &http.Server{
		Addr:    cs.httpAddr,
		Handler: mux,
	}

	httpListener, err := net.Listen("tcp", cs.httpAddr)
	if err != nil {
		cs.grpcServer.Stop()
		_ = l.Close()
		return fmt.Errorf("failed to listen on HTTP addr %s: %w", cs.httpAddr, err)
	}

	go func() {
		if err := cs.httpServer.Serve(httpListener); err != nil && err != http.ErrServerClosed {
			fmt.Printf("HTTP server error: %v\n", err)
		}
	}()

	fmt.Printf("Go Stub Server listening UDS on %s, HTTP control on %s\n", cs.sockPath, cs.httpAddr)
	return nil
}

func (cs *ControlServer) Stop() {
	if cs.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = cs.httpServer.Shutdown(ctx)
	}
	if cs.grpcServer != nil {
		cs.grpcServer.GracefulStop()
	}
	_ = os.Remove(cs.sockPath)
}

func (cs *ControlServer) handleDesiredState(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var ds pb.DesiredState
		defer r.Body.Close()
		dec := json.NewDecoder(r.Body)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			http.Error(w, fmt.Sprintf("invalid json: %v", err), http.StatusBadRequest)
			return
		}
		if err := protojson.Unmarshal(raw, &ds); err != nil {
			http.Error(w, fmt.Sprintf("proto unmarshal error: %v", err), http.StatusBadRequest)
			return
		}
		cs.state.SetDesiredState(&ds)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
		return
	}

	ds := cs.state.GetDesiredState()
	bytes, err := protojson.Marshal(ds)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(bytes)
}

func (cs *ControlServer) handleAlarms(w http.ResponseWriter, r *http.Request) {
	alarms := cs.state.GetAlarms()
	type jsonAlarms struct {
		Alarms []*pb.AlarmEvent `json:"alarms"`
		Count  int              `json:"count"`
	}
	res := jsonAlarms{
		Alarms: alarms,
		Count:  len(alarms),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

func (cs *ControlServer) handleTaskStates(w http.ResponseWriter, r *http.Request) {
	cs.state.mu.RLock()
	defer cs.state.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(cs.state.taskStates)
}

func (cs *ControlServer) handleInstanceStates(w http.ResponseWriter, r *http.Request) {
	cs.state.mu.RLock()
	defer cs.state.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(cs.state.instStates)
}

func (cs *ControlServer) handleTelemetry(w http.ResponseWriter, r *http.Request) {
	tele := cs.state.GetTelemetryList()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"telemetry": tele,
		"count":     len(tele),
	})
}

func (cs *ControlServer) handleOrphanReports(w http.ResponseWriter, r *http.Request) {
	cs.state.mu.RLock()
	defer cs.state.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"reports": cs.state.orphanReports,
		"count":   len(cs.state.orphanReports),
	})
}

func (cs *ControlServer) handleOrphanPolicy(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var payload struct {
			Retain []string `json:"retain"`
			Delete []string `json:"delete"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		cs.state.SetOrphanPolicy(payload.Retain, payload.Delete)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
		return
	}
	retain, deleteIDs := cs.state.GetOrphanPolicy()
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"retain": retain,
		"delete": deleteIDs,
	})
}

func (cs *ControlServer) handleFaultInjection(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var payload struct {
			DelayedACKMs int    `json:"delayed_ack_ms"`
			ReturnCode   string `json:"return_code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		cs.state.mu.Lock()
		cs.state.delayedACKMs = payload.DelayedACKMs
		cs.state.returnCode = payload.ReturnCode
		cs.state.mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
		return
	}
	cs.state.mu.RLock()
	defer cs.state.mu.RUnlock()
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"delayed_ack_ms": cs.state.delayedACKMs,
		"return_code":    cs.state.returnCode,
	})
}

func (cs *ControlServer) handleReset(w http.ResponseWriter, r *http.Request) {
	cs.state.ResetReports()
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func main() {
	sockPath := flag.String("sock", "/tmp/aivision_app_test.sock", "Path to unix domain socket")
	httpAddr := flag.String("http", "127.0.0.1:9099", "HTTP address for control and test query")
	flag.Parse()

	state := NewServerState()
	server := NewControlServer(*sockPath, *httpAddr, state)

	if err := server.Start(); err != nil {
		fmt.Printf("Fatal: %v\n", err)
		os.Exit(1)
	}
	defer server.Stop()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	fmt.Println("Shutting down Go Stub Server...")
}
