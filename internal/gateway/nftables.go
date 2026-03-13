//go:build linux

package gateway

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/google/nftables"
	"github.com/google/nftables/expr"
	"golang.org/x/sys/unix"
)

const (
	sluiceNFTablesTable   = "sluice_mangle"
	sluiceNFTablesChain   = "sluice_intercept_output"
	nftReg1               = 0x1
	defaultSSHDConfigPath = "/etc/ssh/sshd_config"
)

type nftablesConn interface {
	ListTablesOfFamily(family nftables.TableFamily) ([]*nftables.Table, error)
	AddTable(table *nftables.Table) *nftables.Table
	AddChain(chain *nftables.Chain) *nftables.Chain
	AddRule(rule *nftables.Rule) *nftables.Rule
	DelTable(table *nftables.Table)
	Flush() error
}

type nftablesAPI interface {
	New() (nftablesConn, error)
}

type systemNFTables struct{}

func (systemNFTables) New() (nftablesConn, error) {
	return nftables.New()
}

type NftablesManager struct {
	api         nftablesAPI
	table       string
	chain       string
	fwmark      int
	controlMark int
	sshPorts    []int
}

func NewNftablesManager(fwmark, controlMark int) *NftablesManager {
	return &NftablesManager{
		api:         systemNFTables{},
		table:       sluiceNFTablesTable,
		chain:       sluiceNFTablesChain,
		fwmark:      fwmark,
		controlMark: controlMark,
		sshPorts:    detectSSHPorts(defaultSSHDConfigPath),
	}
}

func (m *NftablesManager) Setup() error {
	if err := m.Reconcile(); err != nil {
		return fmt.Errorf("reconcile owned nftables rules: %w", err)
	}

	rollback := func(setupErr error) error {
		reconcileErr := m.Reconcile()
		if reconcileErr != nil {
			return errors.Join(setupErr, fmt.Errorf("rollback nftables setup: %w", reconcileErr))
		}
		return setupErr
	}

	conn, err := m.apiHandle().New()
	if err != nil {
		return fmt.Errorf("open nftables connection: %w", err)
	}

	table := conn.AddTable(&nftables.Table{
		Family: nftables.TableFamilyIPv4,
		Name:   m.tableName(),
	})

	chain := conn.AddChain(&nftables.Chain{
		Name:     m.chainName(),
		Table:    table,
		Type:     nftables.ChainTypeRoute,
		Hooknum:  nftables.ChainHookOutput,
		Priority: nftables.ChainPriorityMangle,
	})

	conn.AddRule(&nftables.Rule{Table: table, Chain: chain, Exprs: loopbackReturnExprs()})
	conn.AddRule(&nftables.Rule{Table: table, Chain: chain, Exprs: establishedRelatedReturnExprs()})

	for _, port := range m.sshPortsValues() {
		conn.AddRule(&nftables.Rule{Table: table, Chain: chain, Exprs: sshBypassExprs(port)})
	}

	conn.AddRule(&nftables.Rule{Table: table, Chain: chain, Exprs: controlMarkBypassExprs(m.controlMarkValue())})

	mark := m.fwmarkValue()
	conn.AddRule(&nftables.Rule{Table: table, Chain: chain, Exprs: protoMarkExprs(unix.IPPROTO_TCP, mark)})
	conn.AddRule(&nftables.Rule{Table: table, Chain: chain, Exprs: protoMarkExprs(unix.IPPROTO_UDP, mark)})

	if err := conn.Flush(); err != nil {
		return rollback(fmt.Errorf("flush nftables setup: %w", err))
	}

	return nil
}

func (m *NftablesManager) Cleanup() error {
	return m.Reconcile()
}

func (m *NftablesManager) Reconcile() error {
	conn, err := m.apiHandle().New()
	if err != nil {
		return fmt.Errorf("open nftables connection: %w", err)
	}

	tables, err := conn.ListTablesOfFamily(nftables.TableFamilyIPv4)
	if err != nil {
		return fmt.Errorf("list ipv4 nftables tables: %w", err)
	}

	var found bool
	for _, table := range tables {
		if table == nil || table.Name != m.tableName() {
			continue
		}

		conn.DelTable(table)
		found = true
	}

	if !found {
		return nil
	}

	if err := conn.Flush(); err != nil && !isNFTablesNotFoundError(err) {
		return fmt.Errorf("delete table %q: %w", m.tableName(), err)
	}

	return nil
}

func isNFTablesNotFoundError(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such file or directory")
}

func loopbackReturnExprs() []expr.Any {
	return []expr.Any{
		&expr.Meta{Key: expr.MetaKeyOIFNAME, Register: nftReg1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: nftReg1, Data: nftInterfaceName("lo")},
		&expr.Verdict{Kind: expr.VerdictReturn},
	}
}

func sshBypassExprs(port int) []expr.Any {
	return []expr.Any{
		&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: nftReg1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: nftReg1, Data: []byte{unix.IPPROTO_TCP}},
		&expr.Payload{DestRegister: nftReg1, Base: expr.PayloadBaseTransportHeader, Offset: 2, Len: 2},
		&expr.Cmp{Op: expr.CmpOpEq, Register: nftReg1, Data: nftU16(uint16(port))},
		&expr.Verdict{Kind: expr.VerdictReturn},
	}
}

func controlMarkBypassExprs(controlMark int) []expr.Any {
	return []expr.Any{
		&expr.Meta{Key: expr.MetaKeyMARK, Register: nftReg1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: nftReg1, Data: nftU32(uint32(controlMark))},
		&expr.Verdict{Kind: expr.VerdictReturn},
	}
}

func establishedRelatedReturnExprs() []expr.Any {
	return []expr.Any{
		&expr.Ct{Key: expr.CtKeySTATE, Register: nftReg1},
		&expr.Bitwise{SourceRegister: nftReg1, DestRegister: nftReg1, Len: 4, Mask: nftU32(expr.CtStateBitESTABLISHED | expr.CtStateBitRELATED), Xor: nftU32(0)},
		&expr.Cmp{Op: expr.CmpOpNeq, Register: nftReg1, Data: nftU32(0)},
		&expr.Verdict{Kind: expr.VerdictReturn},
	}
}

func protoMarkExprs(proto uint8, mark int) []expr.Any {
	return []expr.Any{
		&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: nftReg1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: nftReg1, Data: []byte{proto}},
		&expr.Immediate{Register: nftReg1, Data: nftU32(uint32(mark))},
		&expr.Meta{Key: expr.MetaKeyMARK, SourceRegister: true, Register: nftReg1},
	}
}

func nftU16(v uint16) []byte {
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, v)
	return b
}

func nftU32(v uint32) []byte {
	b := make([]byte, 4)
	binary.NativeEndian.PutUint32(b, v)
	return b
}

func nftInterfaceName(name string) []byte {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return make([]byte, unix.IFNAMSIZ)
	}

	b := make([]byte, unix.IFNAMSIZ)
	copy(b, []byte(trimmed))
	return b
}

func (m *NftablesManager) apiHandle() nftablesAPI {
	if m != nil && m.api != nil {
		return m.api
	}
	return systemNFTables{}
}

func (m *NftablesManager) tableName() string {
	if m != nil && strings.TrimSpace(m.table) != "" {
		return m.table
	}
	return sluiceNFTablesTable
}

func (m *NftablesManager) chainName() string {
	if m != nil && strings.TrimSpace(m.chain) != "" {
		return m.chain
	}
	return sluiceNFTablesChain
}

func (m *NftablesManager) fwmarkValue() int {
	if m != nil && m.fwmark != 0 {
		return m.fwmark
	}
	return defaultFwmark
}

func (m *NftablesManager) controlMarkValue() int {
	if m != nil && m.controlMark != 0 {
		return m.controlMark
	}
	return defaultControlMark
}

func (m *NftablesManager) sshPortsValues() []int {
	if m != nil && len(m.sshPorts) > 0 {
		return append([]int(nil), m.sshPorts...)
	}
	return []int{22}
}

func detectSSHPorts(configPath string) []int {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		configPath = defaultSSHDConfigPath
	}

	seen := make(map[int]struct{})
	if err := collectSSHPortsFromConfig(configPath, seen, make(map[string]struct{})); err != nil {
		return []int{22}
	}

	ports := make([]int, 0, len(seen))
	for port := range seen {
		ports = append(ports, port)
	}

	if len(ports) == 0 {
		return []int{22}
	}

	sort.Ints(ports)
	return ports
}

func collectSSHPortsFromConfig(configPath string, seen map[int]struct{}, visited map[string]struct{}) error {
	absPath, err := filepath.Abs(configPath)
	if err != nil {
		return err
	}

	if _, ok := visited[absPath]; ok {
		return nil
	}
	visited[absPath] = struct{}{}

	file, err := os.Open(absPath)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if idx := strings.Index(line, "#"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
			if line == "" {
				continue
			}
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		switch strings.ToLower(fields[0]) {
		case "port":
			if port, ok := parseSSHDPort(fields[1]); ok {
				seen[port] = struct{}{}
			}
		case "listenaddress":
			if port, ok := parseListenAddressPort(fields[1]); ok {
				seen[port] = struct{}{}
			}
		case "include":
			for _, include := range fields[1:] {
				matches, err := resolveSSHDIncludeGlob(absPath, include)
				if err != nil {
					continue
				}
				for _, match := range matches {
					if err := collectSSHPortsFromConfig(match, seen, visited); err != nil {
						return err
					}
				}
			}
		}
	}

	return scanner.Err()
}

func resolveSSHDIncludeGlob(baseConfigPath, include string) ([]string, error) {
	include = strings.TrimSpace(include)
	if include == "" {
		return nil, errors.New("empty include pattern")
	}

	if !filepath.IsAbs(include) {
		include = filepath.Join(filepath.Dir(baseConfigPath), include)
	}

	return filepath.Glob(include)
}

func parseSSHDPort(value string) (int, bool) {
	port, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || port < 1 || port > 65535 {
		return 0, false
	}
	return port, true
}

func parseListenAddressPort(value string) (int, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}

	if strings.HasPrefix(value, "[") {
		end := strings.LastIndex(value, "]")
		if end < 0 || end+1 >= len(value) || value[end+1] != ':' {
			return 0, false
		}
		return parseSSHDPort(value[end+2:])
	}

	if strings.Count(value, ":") != 1 {
		return 0, false
	}

	_, port, ok := strings.Cut(value, ":")
	if !ok {
		return 0, false
	}

	return parseSSHDPort(port)
}
