package vpn

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type shareFile struct {
	Containers []struct {
		AWG *struct {
			LastConfig string `json:"last_config"`
		} `json:"awg"`
	} `json:"containers"`
	DNS1 string `json:"dns1"`
	DNS2 string `json:"dns2"`
}

func Decode(vpnText string) (string, error) {
	s := strings.TrimSpace(vpnText)
	s = strings.TrimPrefix(s, "vpn://")
	s = strings.ReplaceAll(s, "-", "+")
	s = strings.ReplaceAll(s, "_", "/")
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	case 1:
		return "", fmt.Errorf("corrupt base64 in .vpn (len %% 4 == 1)")
	}
	blob, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return "", fmt.Errorf("base64: %w", err)
	}
	if len(blob) <= 4 {
		return "", fmt.Errorf("vpn payload too short")
	}
	// Qt qCompress: first 4 bytes big-endian size, rest zlib.
	zr, err := zlib.NewReader(bytes.NewReader(blob[4:]))
	if err != nil {
		return "", fmt.Errorf("zlib: %w", err)
	}
	defer zr.Close()
	dec, err := io.ReadAll(zr)
	if err != nil {
		return "", fmt.Errorf("inflate: %w", err)
	}
	var sf shareFile
	if err := json.Unmarshal(dec, &sf); err != nil {
		return "", fmt.Errorf("outer json: %w", err)
	}
	var lastCfg string
	for _, c := range sf.Containers {
		if c.AWG != nil && c.AWG.LastConfig != "" {
			lastCfg = c.AWG.LastConfig
			break
		}
	}
	if lastCfg == "" {
		return "", fmt.Errorf("no AmneziaWG (awg) container in .vpn")
	}
	var inner struct {
		Config string `json:"config"`
	}
	if err := json.Unmarshal([]byte(lastCfg), &inner); err != nil {
		return "", fmt.Errorf("inner json: %w", err)
	}
	if inner.Config == "" {
		return "", fmt.Errorf("no .config text inside last_config")
	}
	dns1 := sf.DNS1
	if dns1 == "" {
		dns1 = "1.1.1.1"
	}
	dns2 := sf.DNS2
	if dns2 == "" {
		dns2 = "1.0.0.1"
	}
	cfg := strings.ReplaceAll(inner.Config, "$PRIMARY_DNS", dns1)
	cfg = strings.ReplaceAll(cfg, "$SECONDARY_DNS", dns2)
	return cfg, nil
}

func ProxyConf(wgPath, bindAddr string) string {
	wgPath = filepath.ToSlash(wgPath)
	return fmt.Sprintf("WGConfig = %s\n\n[http]\nBindAddress = %s\n", wgPath, bindAddr)
}

func WriteRuntime(vpnText, runDir, bindAddr string) (string, error) {
	wg, err := Decode(vpnText)
	if err != nil {
		return "", err
	}
	wgPath := filepath.Join(runDir, "awg.conf")
	if err := os.WriteFile(wgPath, []byte(wg), 0o600); err != nil {
		return "", err
	}
	proxyPath := filepath.Join(runDir, "proxy.conf")
	abs, _ := filepath.Abs(wgPath)
	if err := os.WriteFile(proxyPath, []byte(ProxyConf(abs, bindAddr)), 0o600); err != nil {
		return "", err
	}
	return proxyPath, nil
}
