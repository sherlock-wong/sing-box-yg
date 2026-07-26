package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/sherlock-wong/vps-net-manager/internal/model"
	"github.com/sherlock-wong/vps-net-manager/internal/platform"
	"github.com/sherlock-wong/vps-net-manager/internal/protocol"
	"github.com/sherlock-wong/vps-net-manager/internal/realm"
	"github.com/sherlock-wong/vps-net-manager/internal/render"
)

type fakePorts struct {
	events *[]string
	err    error
}

func (fake fakePorts) CheckPorts(context.Context, model.State) error {
	*fake.events = append(*fake.events, "ports")
	return fake.err
}

type fakeCores struct {
	events *[]string
	err    error
}

func (fake fakeCores) CheckCores(context.Context, render.Output) error {
	*fake.events = append(*fake.events, "cores")
	return fake.err
}

type fakeFirewall struct {
	events      *[]string
	prepareErr  error
	finalizeErr error
}

func (fake fakeFirewall) Prepare(context.Context, []protocol.FirewallRule, []protocol.FirewallRule) (Rollback, error) {
	*fake.events = append(*fake.events, "firewall.prepare")
	if fake.prepareErr != nil {
		return nil, fake.prepareErr
	}
	return func(context.Context) error { *fake.events = append(*fake.events, "firewall.rollback"); return nil }, nil
}
func (fake fakeFirewall) Finalize(context.Context, []protocol.FirewallRule, []protocol.FirewallRule) error {
	*fake.events = append(*fake.events, "firewall.finalize")
	return fake.finalizeErr
}

type fakeStore struct {
	events *[]string
	err    error
}

func (fake fakeStore) Commit(context.Context, []Artifact) (Rollback, error) {
	*fake.events = append(*fake.events, "artifacts.commit")
	if fake.err != nil {
		return nil, fake.err
	}
	return func(context.Context) error { *fake.events = append(*fake.events, "artifacts.rollback"); return nil }, nil
}

type fakeServices struct {
	events *[]string
	err    error
}

type fakeRealmPorts struct{ events *[]string }

func (fake fakeRealmPorts) CheckRealmPorts(context.Context, realm.State) error {
	*fake.events = append(*fake.events, "realm.ports")
	return nil
}

type fakeRealmServices struct {
	events *[]string
	err    error
}

func (fake fakeRealmServices) ActivateRealm(context.Context, bool) (Rollback, error) {
	*fake.events = append(*fake.events, "realm.services.activate")
	if fake.err != nil {
		return nil, fake.err
	}
	return func(context.Context) error {
		*fake.events = append(*fake.events, "realm.services.rollback")
		return nil
	}, nil
}

func (fake fakeServices) Activate(context.Context, CoreSet) (Rollback, error) {
	*fake.events = append(*fake.events, "services.activate")
	if fake.err != nil {
		return nil, fake.err
	}
	return func(context.Context) error { *fake.events = append(*fake.events, "services.rollback"); return nil }, nil
}

func TestApplyCommitsAfterAllPreflightChecks(t *testing.T) {
	events := []string{}
	options := ApplyOptions{StateDirectory: t.TempDir(), Ports: fakePorts{&events, nil}, Cores: fakeCores{&events, nil}, Firewall: fakeFirewall{&events, nil, nil}, Artifacts: fakeStore{&events, nil}, Services: fakeServices{&events, nil}}
	applied, err := options.Apply(context.Background(), appState())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ports", "cores", "firewall.prepare", "artifacts.commit", "services.activate", "firewall.finalize"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
	if !applied.Cores.SingBox || applied.Cores.Xray || len(applied.Rules) != 1 {
		t.Fatalf("applied = %+v", applied)
	}
}

func TestApplyRollsBackArtifactsAndFirewallWhenServiceActivationFails(t *testing.T) {
	events := []string{}
	options := ApplyOptions{StateDirectory: t.TempDir(), Ports: fakePorts{&events, nil}, Cores: fakeCores{&events, nil}, Firewall: fakeFirewall{&events, nil, nil}, Artifacts: fakeStore{&events, nil}, Services: fakeServices{&events, errors.New("start failed")}}
	if _, err := options.Apply(context.Background(), appState()); err == nil {
		t.Fatal("Apply unexpectedly succeeded")
	}
	want := []string{"ports", "cores", "firewall.prepare", "artifacts.commit", "services.activate", "artifacts.rollback", "firewall.rollback"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestApplyStopsBeforeFirewallWhenCoreCheckFails(t *testing.T) {
	events := []string{}
	options := ApplyOptions{StateDirectory: t.TempDir(), Ports: fakePorts{&events, nil}, Cores: fakeCores{&events, errors.New("invalid config")}, Firewall: fakeFirewall{&events, nil, nil}, Artifacts: fakeStore{&events, nil}, Services: fakeServices{&events, nil}}
	if _, err := options.Apply(context.Background(), appState()); err == nil {
		t.Fatal("Apply unexpectedly succeeded")
	}
	if want := []string{"ports", "cores"}; !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestApplyRollsBackAllCommittedPartsWhenFirewallFinalizeFails(t *testing.T) {
	events := []string{}
	options := ApplyOptions{StateDirectory: t.TempDir(), Ports: fakePorts{&events, nil}, Cores: fakeCores{&events, nil}, Firewall: fakeFirewall{&events, nil, errors.New("remove old rule failed")}, Artifacts: fakeStore{&events, nil}, Services: fakeServices{&events, nil}}
	if _, err := options.Apply(context.Background(), appState()); err == nil {
		t.Fatal("Apply unexpectedly succeeded")
	}
	want := []string{"ports", "cores", "firewall.prepare", "artifacts.commit", "services.activate", "firewall.finalize", "artifacts.rollback", "services.rollback", "firewall.rollback"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestFilesystemStoreRollbackRestoresAllFiles(t *testing.T) {
	directory := t.TempDir()
	oldPath := filepath.Join(directory, "state.json")
	if err := os.WriteFile(oldPath, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	rollback, err := (FilesystemStore{}).Commit(context.Background(), []Artifact{{Path: oldPath, Contents: []byte("new"), Mode: 0o600}, {Path: filepath.Join(directory, "generated", "client.json"), Contents: []byte("client"), Mode: 0o600}})
	if err != nil {
		t.Fatal(err)
	}
	if err := rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(oldPath)
	if err != nil || string(contents) != "old" {
		t.Fatalf("state = %q, err = %v", contents, err)
	}
	if _, err := os.Stat(filepath.Join(directory, "generated", "client.json")); !os.IsNotExist(err) {
		t.Fatalf("new artifact remains: %v", err)
	}
}

func TestRealmApplyRollsBackFilesAndFirewallWhenActivationFails(t *testing.T) {
	events := []string{}
	runner := platform.RunnerFunc(func(context.Context, platform.Command) (platform.CommandResult, error) {
		events = append(events, "realm.reload")
		return platform.CommandResult{}, nil
	})
	options := RealmApplyOptions{
		StateDirectory: t.TempDir(), UnitDirectory: t.TempDir(), Ports: fakeRealmPorts{&events},
		Firewall: fakeFirewall{&events, nil, nil}, Artifacts: fakeStore{&events, nil},
		Services: fakeRealmServices{&events, errors.New("realm start failed")}, Runner: runner,
	}
	state := realm.State{Schema: realm.Schema, Rules: []realm.Rule{{ID: "realm_a1b2c3d4", ListenHost: "0.0.0.0", ListenPort: 40123, RemoteHost: "203.0.113.10", RemotePort: 443}}}
	if err := options.Apply(context.Background(), state); err == nil {
		t.Fatal("Realm Apply unexpectedly succeeded")
	}
	want := []string{"realm.ports", "firewall.prepare", "artifacts.commit", "realm.reload", "realm.services.activate", "artifacts.rollback", "firewall.rollback"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}
