package aliyun

import (
	"testing"

	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
)

func TestNewUsesHTTPS(t *testing.T) {
	aliyun, err := New(&Config{
		AccessKeyID:     "access-key-id",
		AccessKeySecret: "access-key-secret",
		Domain:          "ddns.example.com",
	})
	if err != nil {
		t.Fatalf("create Aliyun updater: %v", err)
	}

	if got := aliyun.client.GetConfig().Scheme; got != requests.HTTPS {
		t.Fatalf("expected Aliyun API scheme %q, got %q", requests.HTTPS, got)
	}
}
