package callgraph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildCallGraph_Go(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "callgraph-go-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Math utils file containing local call: Add -> ValidateInput
	mathUtils := `package math
func Add(a, b int) int {
	ValidateInput(a)
	ValidateInput(b)
	return a + b
}

func ValidateInput(v int) {
	// mock validation
}
`

	// Main execution file containing cross-file call: MainProcess -> Add
	mainGo := `package main

import "math"

func MainProcess() {
	result := math.Add(5, 10)
	println(result)
}
`

	_ = os.WriteFile(filepath.Join(tmpDir, "math_utils.go"), []byte(mathUtils), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(mainGo), 0644)

	// Build Call Graph
	cg, err := BuildCallGraph(tmpDir)
	if err != nil {
		t.Fatalf("failed to build call graph: %v", err)
	}

	// Verify all Go function nodes are registered
	expectedNodes := []string{"Add", "ValidateInput", "MainProcess"}
	for _, expected := range expectedNodes {
		if _, ok := cg.Nodes[expected]; !ok {
			t.Errorf("expected Go function node %s to be registered", expected)
		}
	}

	// Verify the complete, multi-file nested callee chain report
	report := cg.GenerateTreeReport("MainProcess", "callee", 3)
	if !strings.Contains(report, "Add") {
		t.Errorf("expected MainProcess callee chain to contain Add: %s", report)
	}
	if !strings.Contains(report, "ValidateInput") {
		t.Errorf("expected MainProcess callee chain to contain ValidateInput: %s", report)
	}

	// Verify reverse caller chain
	upwardReport := cg.GenerateTreeReport("ValidateInput", "caller", 3)
	if !strings.Contains(upwardReport, "Add") {
		t.Errorf("expected ValidateInput callers to contain Add: %s", upwardReport)
	}
	if !strings.Contains(upwardReport, "MainProcess") {
		t.Errorf("expected ValidateInput callers to contain MainProcess: %s", upwardReport)
	}
}

func TestBuildCallGraph_Terraform(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "callgraph-tf-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// File 1: VPC module and main VPC resource
	vpcTf := `
module "vpc" {
  source = "./modules/vpc"
}

resource "aws_vpc" "main" {
  cidr_block = "10.0.0.0/16"
}
`

	// File 2: Subnets & Route tables referencing VPC across files
	subnetsTf := `
resource "aws_subnet" "public_a" {
  vpc_id = aws_vpc.main.id
}

resource "aws_route_table" "route_a" {
  vpc_id   = aws_vpc.main.id
  route_id = module.vpc.route_table_id
}
`

	_ = os.WriteFile(filepath.Join(tmpDir, "vpc.tf"), []byte(vpcTf), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "subnets.tf"), []byte(subnetsTf), 0644)

	// Build Call Graph
	cg, err := BuildCallGraph(tmpDir)
	if err != nil {
		t.Fatalf("failed to build call graph: %v", err)
	}

	// Assert registered nodes
	expectedNodes := []string{
		"module.vpc",
		"resource.aws_vpc.main",
		"resource.aws_subnet.public_a",
		"resource.aws_route_table.route_a",
	}
	for _, expected := range expectedNodes {
		if _, ok := cg.Nodes[expected]; !ok {
			t.Errorf("expected TF node %s to be registered", expected)
		}
	}

	// Verify that the route_table callee chain contains both its cross-file module and resource dependencies
	report := cg.GenerateTreeReport("aws_route_table.route_a", "callee", 2)
	if !strings.Contains(report, "aws_vpc.main") {
		t.Errorf("expected route_table callee chain to contain aws_vpc.main: %s", report)
	}
	if !strings.Contains(report, "module.vpc") {
		t.Errorf("expected route_table callee chain to contain module.vpc: %s", report)
	}
}

func TestBuildCallGraph_YAML(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "callgraph-yaml-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// File 1: CI workflows
	ciYaml := `
steps:
  - name: Build
    run_task: go build
  - name: Test
    needs: [Build]
`

	// File 2: CD workflow referencing test completion across files
	cdYaml := `
steps:
  - name: Deploy
    needs: [Test]
`

	_ = os.WriteFile(filepath.Join(tmpDir, "ci.yaml"), []byte(ciYaml), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "cd.yaml"), []byte(cdYaml), 0644)

	// Build Call Graph
	cg, err := BuildCallGraph(tmpDir)
	if err != nil {
		t.Fatalf("failed to build call graph: %v", err)
	}

	// Assert registered nodes
	expectedNodes := []string{"step.Build", "step.Test", "step.Deploy"}
	for _, expected := range expectedNodes {
		if _, ok := cg.Nodes[expected]; !ok {
			t.Errorf("expected YAML step %s to be registered", expected)
		}
	}

	// Verify step.Deploy depends on step.Test (cross-file) which depends on step.Build (within-file)
	report := cg.GenerateTreeReport("step.Deploy", "callee", 3)
	if !strings.Contains(report, "step.Test") {
		t.Errorf("expected Deploy to depend on Test: %s", report)
	}
	if !strings.Contains(report, "step.Build") {
		t.Errorf("expected Deploy to transitively depend on Build: %s", report)
	}
}
