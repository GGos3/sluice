//go:build linux

package gateway

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	tlsRecordHeaderLen       = 5
	tlsHandshakeTypeClientHi = 1
	tlsRecordTypeHandshake   = 22
	tlsExtServerName         = 0
	tlsNameTypeHostName      = 0
)

func ExtractSNI(reader io.Reader) (string, []byte, error) {
	consumed, err := readExact(reader, tlsRecordHeaderLen)
	if err != nil {
		return "", consumed, fmt.Errorf("read tls record header: %w", err)
	}

	if consumed[0] != tlsRecordTypeHandshake {
		return "", consumed, fmt.Errorf("not a tls handshake record: type=%d", consumed[0])
	}

	recordLen := int(binary.BigEndian.Uint16(consumed[3:5]))
	if recordLen == 0 {
		return "", consumed, fmt.Errorf("empty tls record")
	}

	body, err := readExact(reader, recordLen)
	consumed = append(consumed, body...)
	if err != nil {
		return "", consumed, fmt.Errorf("read tls record body: %w", err)
	}

	serverName, err := parseClientHelloServerName(body)
	if err != nil {
		return "", consumed, err
	}

	return serverName, consumed, nil
}

func readExact(reader io.Reader, size int) ([]byte, error) {
	buf := make([]byte, size)
	total := 0
	for total < size {
		n, err := reader.Read(buf[total:])
		if n > 0 {
			total += n
		}
		if err != nil {
			return buf[:total], err
		}
		if n == 0 {
			return buf[:total], io.ErrUnexpectedEOF
		}
	}
	return buf, nil
}

func parseClientHelloServerName(recordBody []byte) (string, error) {
	if len(recordBody) < 4 {
		return "", fmt.Errorf("tls handshake truncated")
	}
	if recordBody[0] != tlsHandshakeTypeClientHi {
		return "", fmt.Errorf("not a client hello: type=%d", recordBody[0])
	}

	handshakeLen := int(recordBody[1])<<16 | int(recordBody[2])<<8 | int(recordBody[3])
	if handshakeLen > len(recordBody)-4 {
		return "", fmt.Errorf("client hello truncated")
	}
	data := recordBody[4 : 4+handshakeLen]

	if len(data) < 34 {
		return "", fmt.Errorf("client hello missing fixed fields")
	}

	offset := 34

	if offset >= len(data) {
		return "", fmt.Errorf("client hello truncated before session id")
	}
	sessionIDLen := int(data[offset])
	offset++
	if offset+sessionIDLen > len(data) {
		return "", fmt.Errorf("client hello truncated in session id")
	}
	offset += sessionIDLen

	if offset+2 > len(data) {
		return "", fmt.Errorf("client hello truncated before cipher suites")
	}
	cipherSuitesLen := int(binary.BigEndian.Uint16(data[offset : offset+2]))
	offset += 2
	if offset+cipherSuitesLen > len(data) {
		return "", fmt.Errorf("client hello truncated in cipher suites")
	}
	offset += cipherSuitesLen

	if offset >= len(data) {
		return "", fmt.Errorf("client hello truncated before compression methods")
	}
	compressionMethodsLen := int(data[offset])
	offset++
	if offset+compressionMethodsLen > len(data) {
		return "", fmt.Errorf("client hello truncated in compression methods")
	}
	offset += compressionMethodsLen

	if offset == len(data) {
		return "", nil
	}
	if offset+2 > len(data) {
		return "", fmt.Errorf("client hello truncated before extensions")
	}
	extensionsLen := int(binary.BigEndian.Uint16(data[offset : offset+2]))
	offset += 2
	if offset+extensionsLen > len(data) {
		return "", fmt.Errorf("client hello truncated in extensions")
	}

	return parseServerNameExtension(data[offset : offset+extensionsLen])
}

func parseServerNameExtension(extensions []byte) (string, error) {
	for offset := 0; offset < len(extensions); {
		if offset+4 > len(extensions) {
			return "", fmt.Errorf("tls extension header truncated")
		}

		extType := binary.BigEndian.Uint16(extensions[offset : offset+2])
		extLen := int(binary.BigEndian.Uint16(extensions[offset+2 : offset+4]))
		offset += 4
		if offset+extLen > len(extensions) {
			return "", fmt.Errorf("tls extension truncated")
		}

		if extType == tlsExtServerName {
			return parseServerNameList(extensions[offset : offset+extLen])
		}

		offset += extLen
	}

	return "", nil
}

func parseServerNameList(data []byte) (string, error) {
	if len(data) < 2 {
		return "", fmt.Errorf("server name extension truncated")
	}
	listLen := int(binary.BigEndian.Uint16(data[:2]))
	if listLen > len(data)-2 {
		return "", fmt.Errorf("server name list truncated")
	}

	for offset := 2; offset < 2+listLen; {
		if offset+3 > len(data) {
			return "", fmt.Errorf("server name entry truncated")
		}
		nameType := data[offset]
		nameLen := int(binary.BigEndian.Uint16(data[offset+1 : offset+3]))
		offset += 3
		if offset+nameLen > len(data) {
			return "", fmt.Errorf("server name value truncated")
		}
		if nameType == tlsNameTypeHostName {
			return string(data[offset : offset+nameLen]), nil
		}
		offset += nameLen
	}

	return "", nil
}
