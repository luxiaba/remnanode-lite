package xray

import (
	"context"
	"log"
)

type processRuntime struct {
	binary   string
	assetDir string
}

func (m *Manager) preparePanelRuntime(ctx context.Context, geodata any, hasGeodata bool, fallbackBinary string) (processRuntime, error) {
	binary, err := m.prepareCore(ctx, geodata, hasGeodata, fallbackBinary)
	if err != nil {
		return processRuntime{}, err
	}
	assetDir, err := m.prepareGeoData(ctx, geodata, hasGeodata)
	if err != nil {
		return processRuntime{}, err
	}
	return processRuntime{binary: binary, assetDir: assetDir}, nil
}

func (m *Manager) fallbackBinary(process *processState) string {
	if process != nil && process.binary != "" {
		return process.binary
	}
	return m.xrayBin
}

func logInvalidPanelSection(section string, err error) {
	log.Printf("warning: invalid geodata.%s section skipped: %v", section, err)
}
