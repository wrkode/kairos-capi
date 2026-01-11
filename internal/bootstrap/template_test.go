/*
Copyright 2024 The Kairos CAPI Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
implied. See the License for the specific language governing
permissions and limitations under the License.
*/

package bootstrap

import (
	"strings"
	"testing"

	bootstrapv1beta2 "github.com/wrkode/kairos-capi/api/bootstrap/v1beta2"
)

func TestRenderK0sCloudConfig_ControlPlaneSingleNode(t *testing.T) {
	data := TemplateData{
		Role:           "control-plane",
		SingleNode:     true,
		UserName:       "kairos",
		UserPassword:   "kairos",
		UserGroups:     []string{"admin"},
		GitHubUser:     "testuser",
		HostnamePrefix: "metal-",
	}

	result, err := RenderK0sCloudConfig(data)
	if err != nil {
		t.Fatalf("Failed to render template: %v", err)
	}

	// Check for cloud-config header
	if !strings.Contains(result, "#cloud-config") {
		t.Error("Missing cloud-config header")
	}

	// Check for hostname with Kairos templating
	if !strings.Contains(result, "hostname: metal-{{ trunc 4 .MachineID }}") {
		t.Error("Missing or incorrect hostname with Kairos templating")
	}

	// Check for k0s block
	if !strings.Contains(result, "k0s:") {
		t.Error("Missing k0s block")
	}

	// Check for enabled: true
	if !strings.Contains(result, "enabled: true") {
		t.Error("Missing k0s enabled flag")
	}

	// Check for --single arg
	if !strings.Contains(result, "--single") {
		t.Error("Missing --single arg for single-node mode")
	}

	// Check for user configuration
	if !strings.Contains(result, "name: kairos") {
		t.Error("Missing user name")
	}

	// Check for GitHub user
	if !strings.Contains(result, "github:testuser") {
		t.Error("Missing GitHub user SSH key")
	}
}

func TestRenderK0sCloudConfig_ControlPlaneMultiNode(t *testing.T) {
	data := TemplateData{
		Role:           "control-plane",
		SingleNode:     false,
		UserName:       "kairos",
		UserPassword:   "kairos",
		UserGroups:     []string{"admin"},
		HostnamePrefix: "metal-",
	}

	result, err := RenderK0sCloudConfig(data)
	if err != nil {
		t.Fatalf("Failed to render template: %v", err)
	}

	// Check for k0s block
	if !strings.Contains(result, "k0s:") {
		t.Error("Missing k0s block")
	}

	// Should NOT have --single arg
	if strings.Contains(result, "--single") {
		t.Error("Should not have --single arg for multi-node mode")
	}
}

func TestRenderK0sCloudConfig_Worker(t *testing.T) {
	data := TemplateData{
		Role:           "worker",
		SingleNode:     false,
		UserName:       "kairos",
		UserPassword:   "kairos",
		UserGroups:     []string{"admin"},
		WorkerToken:    "test-token-12345",
		HostnamePrefix: "metal-",
	}

	result, err := RenderK0sCloudConfig(data)
	if err != nil {
		t.Fatalf("Failed to render template: %v", err)
	}

	// Check for k0s-worker block
	if !strings.Contains(result, "k0s-worker:") {
		t.Error("Missing k0s-worker block")
	}

	// Check for enabled: true
	if !strings.Contains(result, "enabled: true") {
		t.Error("Missing k0s-worker enabled flag")
	}

	// Check for token file arg
	if !strings.Contains(result, "--token-file /etc/k0s/token") {
		t.Error("Missing --token-file arg")
	}

	// Check for token file write
	if !strings.Contains(result, "path: /etc/k0s/token") {
		t.Error("Missing token file write_files entry")
	}

	// Check for token content
	if !strings.Contains(result, "test-token-12345") {
		t.Error("Missing worker token in file content")
	}

	// Should NOT have k0s block (control-plane)
	if strings.Contains(result, "\nk0s:\n") {
		t.Error("Should not have k0s block for worker")
	}
}

func TestRenderK0sCloudConfig_WithManifests(t *testing.T) {
	data := TemplateData{
		Role:           "control-plane",
		SingleNode:     true,
		UserName:       "kairos",
		UserPassword:   "kairos",
		UserGroups:     []string{"admin"},
		HostnamePrefix: "metal-",
		Manifests: []bootstrapv1beta2.Manifest{
			{
				Name:    "test",
				File:    "test.yaml",
				Content: "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: test",
			},
		},
	}

	result, err := RenderK0sCloudConfig(data)
	if err != nil {
		t.Fatalf("Failed to render template: %v", err)
	}

	// Check for manifest path
	if !strings.Contains(result, "/var/lib/k0s/manifests/test/test.yaml") {
		t.Error("Missing manifest path")
	}

	// Check for manifest content
	if !strings.Contains(result, "kind: Namespace") {
		t.Error("Missing manifest content")
	}
}

func TestRenderK0sCloudConfig_WithSSHPublicKey(t *testing.T) {
	data := TemplateData{
		Role:           "control-plane",
		SingleNode:     true,
		UserName:       "kairos",
		UserPassword:   "kairos",
		UserGroups:     []string{"admin"},
		SSHPublicKey:   "ssh-rsa AAAAB3NzaC1yc2E...",
		HostnamePrefix: "metal-",
	}

	result, err := RenderK0sCloudConfig(data)
	if err != nil {
		t.Fatalf("Failed to render template: %v", err)
	}

	// Check for SSH public key
	if !strings.Contains(result, "ssh-rsa AAAAB3NzaC1yc2E...") {
		t.Error("Missing SSH public key")
	}
}

func TestRenderK0sCloudConfig_WithInstallConfig(t *testing.T) {
	installConfig := &InstallConfig{
		Auto:   true,
		Device: "auto",
		Reboot: true,
	}

	data := TemplateData{
		Role:           "control-plane",
		SingleNode:     true,
		UserName:       "kairos",
		UserPassword:   "kairos",
		UserGroups:     []string{"admin"},
		HostnamePrefix: "metal-",
		Install:        installConfig,
	}

	result, err := RenderK0sCloudConfig(data)
	if err != nil {
		t.Fatalf("Failed to render template: %v", err)
	}

	// Check for install block
	if !strings.Contains(result, "install:") {
		t.Error("Missing install block")
	}

	// Check for install.auto
	if !strings.Contains(result, "auto: true") {
		t.Error("Missing install.auto: true")
	}

	// Check for install.device
	if !strings.Contains(result, "device: \"auto\"") {
		t.Error("Missing install.device: \"auto\"")
	}

	// Check for install.reboot
	if !strings.Contains(result, "reboot: true") {
		t.Error("Missing install.reboot: true")
	}
}

func TestRenderK0sCloudConfig_WithoutInstallConfig(t *testing.T) {
	data := TemplateData{
		Role:           "control-plane",
		SingleNode:     true,
		UserName:       "kairos",
		UserPassword:   "kairos",
		UserGroups:     []string{"admin"},
		HostnamePrefix: "metal-",
		Install:        nil, // No install config
	}

	result, err := RenderK0sCloudConfig(data)
	if err != nil {
		t.Fatalf("Failed to render template: %v", err)
	}

	// Verify install block is NOT present when Install is nil
	if strings.Contains(result, "install:") {
		t.Error("Install block should not be present when Install is nil")
	}
}
