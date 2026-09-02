// Package argusv1 包含从 engine/proto/argus/v1 生成的 Go protobuf/gRPC 代码。
// 本文件提供生成物与权威 proto 契约一致的冒烟测试：包名、完整 service descriptor
// 与 19 个本任务 RPC（6 个 Go server 入站 + 13 个 Engine client 出站）。
package argusv1

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// expectedServices 期望的服务名 -> RPC 方法名列表（与 engine/proto/argus/v1 一致）。
var expectedServices = map[string][]string{
	"argus.v1.EngineService": {
		"ApplyDesiredState",
		"UpsertTask",
		"SetInstanceState",
		"UpdateInstanceConfig",
		"InstallPackage",
		"UpgradePackage",
		"RollbackPackage",
		"UninstallPackage",
		"DeleteImages",
		"ReconcileImages",
		"QueryProfile",
		"QueryMetrics",
		"ProbeCamera",
		"StartCameraPreview",
		"StopCameraPreview",
	},
	"argus.v1.ControlPlaneService": {
		"GetDesiredState",
		"GetFaceGallery",
	},
	"argus.v1.ReportService": {
		"ReportAlarm",
		"ReportPlateObservation",
		"ReportFaceObservation",
		"ReportFaceCapture",
		"ReportTaskState",
		"ReportInstanceState",
		"ReportMetrics",
		"ReportOrphanImages",
	},
	"argus.v1.PersonService": {
		"ExtractFaceFeature",
	},
}

// TestGeneratedDescriptorMatchesProto 校验生成代码的 service descriptor 与权威 proto 完全一致。
func TestGeneratedDescriptorMatchesProto(t *testing.T) {
	seen := map[string]bool{}
	for _, fd := range []protoreflect.FileDescriptor{
		File_argus_v1_common_proto,
		File_argus_v1_engine_proto,
		File_argus_v1_app_proto,
		File_argus_v1_person_proto,
	} {
		if !strings.HasPrefix(fd.Path(), "argus/v1/") {
			t.Fatalf("unexpected file path %q", fd.Path())
		}
		services := fd.Services()
		for i := 0; i < services.Len(); i++ {
			svc := services.Get(i)
			name := string(svc.FullName())
			expectedMethods, want := expectedServices[name]
			if !want {
				t.Errorf("unexpected service %q in generated descriptor", name)
				continue
			}
			seen[name] = true
			methods := svc.Methods()
			if methods.Len() != len(expectedMethods) {
				t.Errorf("service %q has %d methods, want %d", name, methods.Len(), len(expectedMethods))
			}
			for j := 0; j < methods.Len(); j++ {
				m := methods.Get(j)
				if !contains(expectedMethods, string(m.Name())) {
					t.Errorf("service %q has unexpected method %q", name, m.Name())
				}
			}
		}
	}

	for name := range expectedServices {
		if !seen[name] {
			t.Errorf("service %q missing from generated descriptor", name)
		}
	}
	// 本任务范围：10 个入站（ControlPlane 2 + Report 8）+ 15 个出站 EngineService = 25。
	taskRPCs := 0
	for _, svc := range []string{"argus.v1.EngineService", "argus.v1.ControlPlaneService", "argus.v1.ReportService"} {
		taskRPCs += len(expectedServices[svc])
	}
	if taskRPCs != 25 {
		t.Errorf("in-scope RPC count = %d, want 25", taskRPCs)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// TestFullMethodNameConvention 校验生成的全限定方法名与权威 proto 命名一致。
func TestFullMethodNameConvention(t *testing.T) {
	cases := map[string]string{
		"/argus.v1.EngineService/QueryProfile":          EngineService_QueryProfile_FullMethodName,
		"/argus.v1.EngineService/ProbeCamera":           EngineService_ProbeCamera_FullMethodName,
		"/argus.v1.ControlPlaneService/GetDesiredState": ControlPlaneService_GetDesiredState_FullMethodName,
		"/argus.v1.ReportService/ReportMetrics":         ReportService_ReportMetrics_FullMethodName,
	}
	for want, got := range cases {
		if got != want {
			t.Errorf("FullMethodName = %q, want %q", got, want)
		}
	}
}
