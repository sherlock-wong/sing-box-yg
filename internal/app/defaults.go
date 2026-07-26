package app

import (
	"path/filepath"

	"github.com/sherlock-wong/vps-net-manager/internal/model"
)

// DefaultApplyOptions describes the filesystem layout created by vpnm install.
// It deliberately performs no dependency download: a missing core or unit is
// a preflight error rather than an implicit installation side effect.
func DefaultApplyOptions(stateDirectory string, previous *model.State) ApplyOptions {
	return ApplyOptions{
		StateDirectory: stateDirectory,
		Previous:       previous,
		Ports:          NoopPortChecker{},
		Cores: CommandCoreChecker{
			SingBoxPath: filepath.Join(stateDirectory, "bin", "sing-box"),
			XrayPath:    filepath.Join(stateDirectory, "bin", "xray"),
		},
		Firewall:  UFWController{},
		Artifacts: FilesystemStore{},
		Services:  SystemdServiceController{},
		NAT:       Hy2NATController{},
	}
}

func DefaultRealmApplyOptions(stateDirectory string) RealmApplyOptions {
	return RealmApplyOptions{
		StateDirectory: stateDirectory,
		UnitDirectory:  "/etc/systemd/system",
		Ports:          NoopRealmPortChecker{},
		Firewall:       UFWController{},
		Artifacts:      FilesystemStore{},
		Services:       SystemdRealmServiceController{},
	}
}
