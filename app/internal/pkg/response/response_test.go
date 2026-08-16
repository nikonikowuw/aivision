package response

import (
	"encoding/json"
	"testing"
)

func TestOKJSON(t *testing.T) {
	got, err := json.Marshal(OK("x"))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"code":0,"data":"x","message":"ok"}`
	if string(got) != want {
		t.Errorf("OK json = %s, want %s", got, want)
	}
}

func TestFailJSON(t *testing.T) {
	got, err := json.Marshal(Fail(1001, "用户名或密码错误"))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"code":1001,"data":null,"message":"用户名或密码错误"}`
	if string(got) != want {
		t.Errorf("Fail json = %s, want %s", got, want)
	}
}
