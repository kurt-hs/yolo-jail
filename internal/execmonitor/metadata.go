// Package execmonitor contains the fail-closed Linux Podman exec-monitor recovery predicate.
package execmonitor

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// RuntimeInfo is the small, version-fenced part of `podman info --format json` used by recovery.
type RuntimeInfo struct {
	Version         string
	ConmonVersion   string
	ServiceIsRemote bool
	ConmonPath      string
}

// Container is the typed inspect snapshot needed to associate an exec monitor with one container.
type Container struct {
	ID      string
	State   string
	ExecIDs []string
}

// ParseRuntimeInfo accepts only the inspected Podman 5.8.9 JSON schema and explicit local client
// selection. ServiceIsRemote is set by that version's client command, not copied from the server.
func ParseRuntimeInfo(raw []byte) (RuntimeInfo, error) {
	root, err := strictJSON(raw)
	if err != nil {
		return RuntimeInfo{}, err
	}
	version, ok := root["version"].(map[string]any)
	if !ok {
		return RuntimeInfo{}, errors.New("podman info has no Version object")
	}
	versionText, ok := version["Version"].(string)
	if !ok || versionText != "5.8.9" {
		return RuntimeInfo{}, errors.New("podman info version is missing or unsupported")
	}
	host, ok := root["host"].(map[string]any)
	if !ok {
		return RuntimeInfo{}, errors.New("podman info has no host object")
	}
	remote, ok := host["serviceIsRemote"].(bool)
	if !ok || remote {
		return RuntimeInfo{}, errors.New("podman info does not prove this client is local")
	}
	conmon, ok := host["conmon"].(map[string]any)
	if !ok {
		return RuntimeInfo{}, errors.New("podman info has no conmon object")
	}
	path, ok := conmon["path"].(string)
	if !ok || path == "" || !strings.HasPrefix(path, "/") {
		return RuntimeInfo{}, errors.New("podman info has no absolute Conmon.Path")
	}
	conmonVersionText, ok := conmon["version"].(string)
	if !ok {
		return RuntimeInfo{}, errors.New("podman info has missing or unsupported conmon version")
	}
	versionAndCommit, ok := strings.CutPrefix(conmonVersionText, "conmon version ")
	if !ok {
		return RuntimeInfo{}, errors.New("podman info has malformed conmon version output")
	}
	semanticVersion, commit, ok := strings.Cut(versionAndCommit, ", commit: ")
	if !ok || semanticVersion != "2.2.1" || strings.TrimSpace(commit) == "" || strings.ContainsAny(commit, "\r\n") {
		return RuntimeInfo{}, errors.New("podman info has missing or unsupported conmon version")
	}
	return RuntimeInfo{Version: versionText, ConmonVersion: semanticVersion, ServiceIsRemote: remote, ConmonPath: path}, nil
}

// ParseContainer accepts the v5.8.9 `{{json .}}` inspect object. The ID is the full ID supplied
// by typed container metadata; status is Podman's explicit state word.
func ParseContainer(raw []byte) (Container, error) {
	root, err := strictJSON(raw)
	if err != nil {
		return Container{}, err
	}
	id, ok := root["Id"].(string)
	if !ok || len(id) != 64 || strings.Trim(id, "0123456789abcdef") != "" {
		return Container{}, errors.New("typed inspect has no exact full lowercase container ID")
	}
	state, ok := root["State"].(map[string]any)
	if !ok {
		return Container{}, errors.New("typed inspect has no State object")
	}
	status, ok := state["Status"].(string)
	if !ok || status == "" {
		return Container{}, errors.New("typed inspect has no state status")
	}
	value, ok := root["ExecIDs"].([]any)
	if !ok {
		return Container{}, errors.New("typed inspect has no ExecIDs array")
	}
	execs := make([]string, 0, len(value))
	for _, item := range value {
		id, ok := item.(string)
		if !ok || id == "" {
			return Container{}, errors.New("typed inspect has malformed ExecIDs")
		}
		execs = append(execs, id)
	}
	return Container{ID: id, State: status, ExecIDs: execs}, nil
}

// strictJSON parses JSON objects while rejecting duplicate keys at every depth. Unknown fields are
// allowed because Podman info and inspect contain much more data than this safety predicate uses.
func strictJSON(raw []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	value, err := decodeValue(dec)
	if err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("JSON has trailing data")
	}
	root, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("JSON root is not an object")
	}
	return root, nil
}

func decodeValue(dec *json.Decoder) (any, error) {
	token, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return token, nil
	}
	switch delim {
	case '{':
		object := map[string]any{}
		for dec.More() {
			keyToken, err := dec.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, errors.New("object key is not a string")
			}
			if _, duplicate := object[key]; duplicate {
				return nil, fmt.Errorf("duplicate JSON object key %q", key)
			}
			value, err := decodeValue(dec)
			if err != nil {
				return nil, err
			}
			object[key] = value
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		return object, nil
	case '[':
		var values []any
		for dec.More() {
			value, err := decodeValue(dec)
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		return values, nil
	default:
		return nil, fmt.Errorf("unexpected JSON delimiter %q", delim)
	}
}
