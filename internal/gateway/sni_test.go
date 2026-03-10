//go:build linux

package gateway

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

func TestExtractSNI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		payload   []byte
		wantSNI   string
		wantError bool
	}{
		{
			name:    "client hello with sni",
			payload: buildTLSClientHelloRecordForSNI("example.com"),
			wantSNI: "example.com",
		},
		{
			name:    "client hello without sni",
			payload: buildTLSClientHelloRecordForSNI(""),
		},
		{
			name:      "non tls payload",
			payload:   []byte("hello world"),
			wantError: true,
		},
		{
			name:      "truncated handshake",
			payload:   buildTLSClientHelloRecordForSNI("example.com")[:9],
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotSNI, replay, err := ExtractSNI(bytes.NewReader(tt.payload))
			if tt.wantError {
				if err == nil {
					t.Fatal("ExtractSNI() error = nil, want non-nil")
				}
			} else if err != nil {
				t.Fatalf("ExtractSNI() error = %v", err)
			}

			if gotSNI != tt.wantSNI {
				t.Fatalf("ExtractSNI() sni = %q, want %q", gotSNI, tt.wantSNI)
			}

			if !bytes.Equal(replay, tt.payload[:len(replay)]) {
				t.Fatalf("replay bytes changed: got %x want prefix %x", replay, tt.payload[:len(replay)])
			}

			if !tt.wantError && !bytes.Equal(replay, tt.payload) {
				t.Fatalf("replay bytes = %x, want %x", replay, tt.payload)
			}
		})
	}
}

func TestExtractSNIPartialReplay(t *testing.T) {
	t.Parallel()

	payload := buildTLSClientHelloRecordForSNI("example.com")[:7]
	_, replay, err := ExtractSNI(bytes.NewReader(payload))
	if !errors.Is(err, io.ErrUnexpectedEOF) && err == nil {
		t.Fatalf("ExtractSNI() error = %v, want unexpected EOF", err)
	}
	if !bytes.Equal(replay, payload) {
		t.Fatalf("replay bytes = %x, want %x", replay, payload)
	}
}

func buildTLSClientHelloRecordForSNI(serverName string) []byte {
	handshakeBody := make([]byte, 0, 128)
	handshakeBody = append(handshakeBody, 0x03, 0x03)
	handshakeBody = append(handshakeBody, bytes.Repeat([]byte{0x01}, 32)...)
	handshakeBody = append(handshakeBody, 0x00)
	handshakeBody = binary.BigEndian.AppendUint16(handshakeBody, 2)
	handshakeBody = append(handshakeBody, 0x13, 0x01)
	handshakeBody = append(handshakeBody, 0x01, 0x00)

	extensions := make([]byte, 0, 64)
	if serverName != "" {
		nameBytes := []byte(serverName)
		serverNameData := make([]byte, 0, len(nameBytes)+5)
		serverNameData = binary.BigEndian.AppendUint16(serverNameData, uint16(len(nameBytes)+3))
		serverNameData = append(serverNameData, 0x00)
		serverNameData = binary.BigEndian.AppendUint16(serverNameData, uint16(len(nameBytes)))
		serverNameData = append(serverNameData, nameBytes...)

		extensions = binary.BigEndian.AppendUint16(extensions, 0)
		extensions = binary.BigEndian.AppendUint16(extensions, uint16(len(serverNameData)))
		extensions = append(extensions, serverNameData...)
	}

	handshakeBody = binary.BigEndian.AppendUint16(handshakeBody, uint16(len(extensions)))
	handshakeBody = append(handshakeBody, extensions...)

	handshake := []byte{0x01, byte(len(handshakeBody) >> 16), byte(len(handshakeBody) >> 8), byte(len(handshakeBody))}
	handshake = append(handshake, handshakeBody...)

	record := []byte{0x16, 0x03, 0x01, byte(len(handshake) >> 8), byte(len(handshake))}
	record = append(record, handshake...)
	return record
}
