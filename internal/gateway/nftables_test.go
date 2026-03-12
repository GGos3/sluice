//go:build linux

package gateway

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/google/nftables"
	"github.com/google/nftables/expr"
	"golang.org/x/sys/unix"
)

func TestNftablesSetupCreatesOwnedRulesInOrder(t *testing.T) {
	t.Parallel()

	cleanupConn := &fakeNFTablesConn{
		tables: []*nftables.Table{
			{Family: nftables.TableFamilyIPv4, Name: "keep_me"},
			{Family: nftables.TableFamilyIPv4, Name: "test_table"},
		},
	}
	setupConn := &fakeNFTablesConn{}
	api := &fakeNFTablesAPI{conns: []*fakeNFTablesConn{cleanupConn, setupConn}}

	mgr := &NftablesManager{
		api:      api,
		table:    "test_table",
		chain:    "test_chain",
		fwmark:   0x9,
		sshPorts: []int{22, 220},
	}

	if err := mgr.Setup(); err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	if got, want := cleanupConn.operations, []string{"ListTablesOfFamily", "DelTable", "Flush"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("cleanup operations = %#v, want %#v", got, want)
	}
	if got, want := len(cleanupConn.deletedTables), 1; got != want {
		t.Fatalf("deletedTables = %d, want %d", got, want)
	}
	if got, want := cleanupConn.deletedTables[0].Name, "test_table"; got != want {
		t.Fatalf("deletedTables[0].Name = %q, want %q", got, want)
	}

	if got, want := setupConn.operations, []string{"AddTable", "AddChain", "AddRule", "AddRule", "AddRule", "AddRule", "AddRule", "AddRule", "Flush"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("setup operations = %#v, want %#v", got, want)
	}
	if got, want := len(setupConn.addedTables), 1; got != want {
		t.Fatalf("addedTables = %d, want %d", got, want)
	}
	if got, want := len(setupConn.addedChains), 1; got != want {
		t.Fatalf("addedChains = %d, want %d", got, want)
	}
	if got, want := len(setupConn.addedRules), 6; got != want {
		t.Fatalf("addedRules = %d, want %d", got, want)
	}

	table := setupConn.addedTables[0]
	if got, want := table.Family, nftables.TableFamilyIPv4; got != want {
		t.Fatalf("table.Family = %v, want %v", got, want)
	}
	if got, want := table.Name, "test_table"; got != want {
		t.Fatalf("table.Name = %q, want %q", got, want)
	}

	chain := setupConn.addedChains[0]
	if got, want := chain.Name, "test_chain"; got != want {
		t.Fatalf("chain.Name = %q, want %q", got, want)
	}
	if chain.Table != table {
		t.Fatalf("chain.Table = %#v, want added table pointer", chain.Table)
	}
	if got, want := chain.Type, nftables.ChainTypeRoute; got != want {
		t.Fatalf("chain.Type = %v, want %v", got, want)
	}
	if got, want := chain.Hooknum, nftables.ChainHookOutput; got != want {
		t.Fatalf("chain.Hooknum = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(chain.Priority, nftables.ChainPriorityMangle) {
		t.Fatalf("chain.Priority = %#v, want %#v", chain.Priority, nftables.ChainPriorityMangle)
	}

	assertLoopbackRule(t, setupConn.addedRules[0], table, chain)
	assertEstablishedRelatedRule(t, setupConn.addedRules[1], table, chain)
	assertSSHBypassRule(t, setupConn.addedRules[2], table, chain, 22)
	assertSSHBypassRule(t, setupConn.addedRules[3], table, chain, 220)
	assertMarkRule(t, setupConn.addedRules[4], table, chain, unix.IPPROTO_TCP, 0x9)
	assertMarkRule(t, setupConn.addedRules[5], table, chain, unix.IPPROTO_UDP, 0x9)
	if got, want := setupConn.flushCalls, 1; got != want {
		t.Fatalf("flushCalls = %d, want %d", got, want)
	}
}

func TestNftablesSetupRollsBackOnFlushError(t *testing.T) {
	t.Parallel()

	cleanupConn := &fakeNFTablesConn{}
	setupConn := &fakeNFTablesConn{flushErr: errors.New("boom")}
	rollbackConn := &fakeNFTablesConn{tables: []*nftables.Table{{Family: nftables.TableFamilyIPv4, Name: sluiceNFTablesTable}}}
	api := &fakeNFTablesAPI{conns: []*fakeNFTablesConn{cleanupConn, setupConn, rollbackConn}}

	mgr := &NftablesManager{api: api}
	err := mgr.Setup()
	if err == nil {
		t.Fatal("Setup() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "flush nftables setup: boom") {
		t.Fatalf("Setup() error = %q, want flush error", err)
	}

	if got, want := rollbackConn.operations, []string{"ListTablesOfFamily", "DelTable", "Flush"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rollback operations = %#v, want %#v", got, want)
	}
	if got, want := len(rollbackConn.deletedTables), 1; got != want {
		t.Fatalf("deletedTables = %d, want %d", got, want)
	}
	if got, want := rollbackConn.deletedTables[0].Name, sluiceNFTablesTable; got != want {
		t.Fatalf("deletedTables[0].Name = %q, want %q", got, want)
	}
}

func TestNftablesCleanupDeletesOwnedTable(t *testing.T) {
	t.Parallel()

	conn := &fakeNFTablesConn{
		tables: []*nftables.Table{
			{Family: nftables.TableFamilyIPv4, Name: "other"},
			{Family: nftables.TableFamilyIPv4, Name: sluiceNFTablesTable},
		},
	}
	api := &fakeNFTablesAPI{conns: []*fakeNFTablesConn{conn}}

	mgr := &NftablesManager{api: api}
	if err := mgr.Cleanup(); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}

	if got, want := conn.operations, []string{"ListTablesOfFamily", "DelTable", "Flush"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("operations = %#v, want %#v", got, want)
	}
	if got, want := len(conn.deletedTables), 1; got != want {
		t.Fatalf("deletedTables = %d, want %d", got, want)
	}
}

func TestNftablesCleanupWithNoOwnedTableIsNoOp(t *testing.T) {
	t.Parallel()

	conn := &fakeNFTablesConn{
		tables: []*nftables.Table{{Family: nftables.TableFamilyIPv4, Name: "other"}},
	}
	api := &fakeNFTablesAPI{conns: []*fakeNFTablesConn{conn}}

	mgr := &NftablesManager{api: api}
	if err := mgr.Cleanup(); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}

	if got, want := conn.operations, []string{"ListTablesOfFamily"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("operations = %#v, want %#v", got, want)
	}
	if got := conn.flushCalls; got != 0 {
		t.Fatalf("flushCalls = %d, want 0", got)
	}
	if got := len(conn.deletedTables); got != 0 {
		t.Fatalf("deletedTables = %d, want 0", got)
	}
}

func TestNftablesSetupUsesDefaultFwmark(t *testing.T) {
	t.Parallel()

	cleanupConn := &fakeNFTablesConn{}
	setupConn := &fakeNFTablesConn{}
	api := &fakeNFTablesAPI{conns: []*fakeNFTablesConn{cleanupConn, setupConn}}

	mgr := &NftablesManager{api: api, fwmark: 0}
	if err := mgr.Setup(); err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	if got, want := len(setupConn.addedRules), 5; got != want {
		t.Fatalf("addedRules = %d, want %d", got, want)
	}
	assertSSHBypassRule(t, setupConn.addedRules[2], setupConn.addedTables[0], setupConn.addedChains[0], 22)
	assertMarkRule(t, setupConn.addedRules[3], setupConn.addedTables[0], setupConn.addedChains[0], unix.IPPROTO_TCP, defaultFwmark)
	assertMarkRule(t, setupConn.addedRules[4], setupConn.addedTables[0], setupConn.addedChains[0], unix.IPPROTO_UDP, defaultFwmark)
}

func TestNftablesReconcileIsIdempotent(t *testing.T) {
	t.Parallel()

	conn := &fakeNFTablesConn{tables: []*nftables.Table{{Family: nftables.TableFamilyIPv4, Name: sluiceNFTablesTable}}}
	api := &fakeNFTablesAPI{conns: []*fakeNFTablesConn{conn, conn}}

	mgr := &NftablesManager{api: api}
	if err := mgr.Reconcile(); err != nil {
		t.Fatalf("Reconcile() first call error = %v", err)
	}
	if err := mgr.Reconcile(); err != nil {
		t.Fatalf("Reconcile() second call error = %v", err)
	}

	if got, want := len(conn.deletedTables), 1; got != want {
		t.Fatalf("deletedTables = %d, want %d", got, want)
	}
	if got, want := conn.flushCalls, 1; got != want {
		t.Fatalf("flushCalls = %d, want %d", got, want)
	}
	if got, want := conn.operations, []string{"ListTablesOfFamily", "DelTable", "Flush", "ListTablesOfFamily"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("operations = %#v, want %#v", got, want)
	}
}

func TestDetectSSHPortsFollowsIncludeDirective(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	includeDir := filepath.Join(baseDir, "sshd_config.d")
	if err := os.MkdirAll(includeDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	mainConfig := filepath.Join(baseDir, "sshd_config")
	if err := os.WriteFile(mainConfig, []byte("Port 22\nInclude sshd_config.d/*.conf\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(main config) error = %v", err)
	}

	includeConfig := filepath.Join(includeDir, "custom.conf")
	if err := os.WriteFile(includeConfig, []byte("Port 220\nListenAddress 0.0.0.0:2022\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(include config) error = %v", err)
	}

	got := detectSSHPorts(mainConfig)
	want := []int{22, 220, 2022}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("detectSSHPorts() = %v, want %v", got, want)
	}
}

func TestDetectSSHPortsSkipsInvalidIncludePattern(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	mainConfig := filepath.Join(baseDir, "sshd_config")
	if err := os.WriteFile(mainConfig, []byte("Port 22\nInclude [broken\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(main config) error = %v", err)
	}

	got := detectSSHPorts(mainConfig)
	want := []int{22}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("detectSSHPorts() = %v, want %v", got, want)
	}
}

func TestCollectSSHPortsFromConfigAvoidsIncludeCycles(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	first := filepath.Join(baseDir, "first.conf")
	second := filepath.Join(baseDir, "second.conf")

	firstData := fmt.Sprintf("Port 22\nInclude %s\n", second)
	if err := os.WriteFile(first, []byte(firstData), 0o644); err != nil {
		t.Fatalf("WriteFile(first) error = %v", err)
	}

	secondData := fmt.Sprintf("Port 220\nInclude %s\n", first)
	if err := os.WriteFile(second, []byte(secondData), 0o644); err != nil {
		t.Fatalf("WriteFile(second) error = %v", err)
	}

	seen := make(map[int]struct{})
	if err := collectSSHPortsFromConfig(first, seen, make(map[string]struct{})); err != nil {
		t.Fatalf("collectSSHPortsFromConfig() error = %v", err)
	}

	if _, ok := seen[22]; !ok {
		t.Fatal("port 22 not collected")
	}
	if _, ok := seen[220]; !ok {
		t.Fatal("port 220 not collected")
	}
}

func TestDetectSSHPortsFallsBackWhenIncludedFileUnreadable(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	mainConfig := filepath.Join(baseDir, "sshd_config")
	brokenTarget := filepath.Join(baseDir, "missing-target.conf")
	includeLink := filepath.Join(baseDir, "broken-include.conf")
	if err := os.Symlink(brokenTarget, includeLink); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	config := fmt.Sprintf("Port 2022\nInclude %s\n", includeLink)
	if err := os.WriteFile(mainConfig, []byte(config), 0o644); err != nil {
		t.Fatalf("WriteFile(main config) error = %v", err)
	}

	got := detectSSHPorts(mainConfig)
	want := []int{22}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("detectSSHPorts() = %v, want %v", got, want)
	}
}

type fakeNFTablesAPI struct {
	conns    []*fakeNFTablesConn
	newCalls int
	newErr   error
}

func (f *fakeNFTablesAPI) New() (nftablesConn, error) {
	if f.newErr != nil {
		return nil, f.newErr
	}
	if f.newCalls >= len(f.conns) {
		return nil, errors.New("unexpected nftables connection")
	}
	conn := f.conns[f.newCalls]
	f.newCalls++
	return conn, nil
}

type fakeNFTablesConn struct {
	tables        []*nftables.Table
	listFamilies  []nftables.TableFamily
	operations    []string
	addedTables   []*nftables.Table
	addedChains   []*nftables.Chain
	addedRules    []*nftables.Rule
	deletedTables []*nftables.Table
	flushCalls    int
	listTablesErr error
	flushErr      error
}

func (f *fakeNFTablesConn) ListTablesOfFamily(family nftables.TableFamily) ([]*nftables.Table, error) {
	f.operations = append(f.operations, "ListTablesOfFamily")
	f.listFamilies = append(f.listFamilies, family)
	if f.listTablesErr != nil {
		return nil, f.listTablesErr
	}
	return append([]*nftables.Table(nil), f.tables...), nil
}

func (f *fakeNFTablesConn) AddTable(table *nftables.Table) *nftables.Table {
	f.operations = append(f.operations, "AddTable")
	f.addedTables = append(f.addedTables, table)
	return table
}

func (f *fakeNFTablesConn) AddChain(chain *nftables.Chain) *nftables.Chain {
	f.operations = append(f.operations, "AddChain")
	f.addedChains = append(f.addedChains, chain)
	return chain
}

func (f *fakeNFTablesConn) AddRule(rule *nftables.Rule) *nftables.Rule {
	f.operations = append(f.operations, "AddRule")
	f.addedRules = append(f.addedRules, rule)
	return rule
}

func (f *fakeNFTablesConn) DelTable(table *nftables.Table) {
	f.operations = append(f.operations, "DelTable")
	f.deletedTables = append(f.deletedTables, table)
	filtered := f.tables[:0]
	for _, existing := range f.tables {
		if existing == nil {
			continue
		}
		if existing == table || (existing.Family == table.Family && existing.Name == table.Name) {
			continue
		}
		filtered = append(filtered, existing)
	}
	f.tables = filtered
}

func (f *fakeNFTablesConn) Flush() error {
	f.operations = append(f.operations, "Flush")
	f.flushCalls++
	return f.flushErr
}

func assertLoopbackRule(t *testing.T, rule *nftables.Rule, table *nftables.Table, chain *nftables.Chain) {
	t.Helper()
	assertRuleLocation(t, rule, table, chain)
	if got, want := len(rule.Exprs), 3; got != want {
		t.Fatalf("len(rule.Exprs) = %d, want %d", got, want)
	}

	meta, ok := rule.Exprs[0].(*expr.Meta)
	if !ok {
		t.Fatalf("expr[0] = %T, want *expr.Meta", rule.Exprs[0])
	}
	if got, want := meta.Key, expr.MetaKeyOIFNAME; got != want {
		t.Fatalf("meta.Key = %v, want %v", got, want)
	}
	if got, want := meta.Register, uint32(nftReg1); got != want {
		t.Fatalf("meta.Register = %d, want %d", got, want)
	}

	cmp, ok := rule.Exprs[1].(*expr.Cmp)
	if !ok {
		t.Fatalf("expr[1] = %T, want *expr.Cmp", rule.Exprs[1])
	}
	if got, want := cmp.Op, expr.CmpOpEq; got != want {
		t.Fatalf("cmp.Op = %v, want %v", got, want)
	}
	if got, want := cmp.Register, uint32(nftReg1); got != want {
		t.Fatalf("cmp.Register = %d, want %d", got, want)
	}
	if got, want := cmp.Data, nftInterfaceName("lo"); !reflect.DeepEqual(got, want) {
		t.Fatalf("cmp.Data = %#v, want %#v", got, want)
	}

	verdict, ok := rule.Exprs[2].(*expr.Verdict)
	if !ok {
		t.Fatalf("expr[2] = %T, want *expr.Verdict", rule.Exprs[2])
	}
	if got, want := verdict.Kind, expr.VerdictReturn; got != want {
		t.Fatalf("verdict.Kind = %v, want %v", got, want)
	}
}

func assertEstablishedRelatedRule(t *testing.T, rule *nftables.Rule, table *nftables.Table, chain *nftables.Chain) {
	t.Helper()
	assertRuleLocation(t, rule, table, chain)
	if got, want := len(rule.Exprs), 4; got != want {
		t.Fatalf("len(rule.Exprs) = %d, want %d", got, want)
	}

	ct, ok := rule.Exprs[0].(*expr.Ct)
	if !ok {
		t.Fatalf("expr[0] = %T, want *expr.Ct", rule.Exprs[0])
	}
	if got, want := ct.Key, expr.CtKeySTATE; got != want {
		t.Fatalf("ct.Key = %v, want %v", got, want)
	}
	if got, want := ct.Register, uint32(nftReg1); got != want {
		t.Fatalf("ct.Register = %d, want %d", got, want)
	}

	bitwise, ok := rule.Exprs[1].(*expr.Bitwise)
	if !ok {
		t.Fatalf("expr[1] = %T, want *expr.Bitwise", rule.Exprs[1])
	}
	if got, want := bitwise.SourceRegister, uint32(nftReg1); got != want {
		t.Fatalf("bitwise.SourceRegister = %d, want %d", got, want)
	}
	if got, want := bitwise.DestRegister, uint32(nftReg1); got != want {
		t.Fatalf("bitwise.DestRegister = %d, want %d", got, want)
	}
	if got, want := bitwise.Len, uint32(4); got != want {
		t.Fatalf("bitwise.Len = %d, want %d", got, want)
	}
	if got, want := bitwise.Mask, nftU32(expr.CtStateBitESTABLISHED|expr.CtStateBitRELATED); !reflect.DeepEqual(got, want) {
		t.Fatalf("bitwise.Mask = %#v, want %#v", got, want)
	}
	if got, want := bitwise.Xor, nftU32(0); !reflect.DeepEqual(got, want) {
		t.Fatalf("bitwise.Xor = %#v, want %#v", got, want)
	}

	cmp, ok := rule.Exprs[2].(*expr.Cmp)
	if !ok {
		t.Fatalf("expr[2] = %T, want *expr.Cmp", rule.Exprs[2])
	}
	if got, want := cmp.Op, expr.CmpOpNeq; got != want {
		t.Fatalf("cmp.Op = %v, want %v", got, want)
	}
	if got, want := cmp.Data, nftU32(0); !reflect.DeepEqual(got, want) {
		t.Fatalf("cmp.Data = %#v, want %#v", got, want)
	}

	verdict, ok := rule.Exprs[3].(*expr.Verdict)
	if !ok {
		t.Fatalf("expr[3] = %T, want *expr.Verdict", rule.Exprs[3])
	}
	if got, want := verdict.Kind, expr.VerdictReturn; got != want {
		t.Fatalf("verdict.Kind = %v, want %v", got, want)
	}
}

func assertSSHBypassRule(t *testing.T, rule *nftables.Rule, table *nftables.Table, chain *nftables.Chain, port int) {
	t.Helper()
	assertRuleLocation(t, rule, table, chain)
	if got, want := len(rule.Exprs), 5; got != want {
		t.Fatalf("len(rule.Exprs) = %d, want %d", got, want)
	}

	meta, ok := rule.Exprs[0].(*expr.Meta)
	if !ok {
		t.Fatalf("expr[0] = %T, want *expr.Meta", rule.Exprs[0])
	}
	if got, want := meta.Key, expr.MetaKeyL4PROTO; got != want {
		t.Fatalf("meta.Key = %v, want %v", got, want)
	}

	protoCmp, ok := rule.Exprs[1].(*expr.Cmp)
	if !ok {
		t.Fatalf("expr[1] = %T, want *expr.Cmp", rule.Exprs[1])
	}
	if got, want := protoCmp.Op, expr.CmpOpEq; got != want {
		t.Fatalf("protoCmp.Op = %v, want %v", got, want)
	}
	if got, want := protoCmp.Data, []byte{unix.IPPROTO_TCP}; !reflect.DeepEqual(got, want) {
		t.Fatalf("protoCmp.Data = %#v, want %#v", got, want)
	}

	payload, ok := rule.Exprs[2].(*expr.Payload)
	if !ok {
		t.Fatalf("expr[2] = %T, want *expr.Payload", rule.Exprs[2])
	}
	if got, want := payload.DestRegister, uint32(nftReg1); got != want {
		t.Fatalf("payload.DestRegister = %d, want %d", got, want)
	}
	if got, want := payload.Base, expr.PayloadBaseTransportHeader; got != want {
		t.Fatalf("payload.Base = %v, want %v", got, want)
	}
	if got, want := payload.Offset, uint32(2); got != want {
		t.Fatalf("payload.Offset = %d, want %d", got, want)
	}
	if got, want := payload.Len, uint32(2); got != want {
		t.Fatalf("payload.Len = %d, want %d", got, want)
	}

	portCmp, ok := rule.Exprs[3].(*expr.Cmp)
	if !ok {
		t.Fatalf("expr[3] = %T, want *expr.Cmp", rule.Exprs[3])
	}
	if got, want := portCmp.Op, expr.CmpOpEq; got != want {
		t.Fatalf("portCmp.Op = %v, want %v", got, want)
	}
	if got, want := portCmp.Data, nftU16(uint16(port)); !reflect.DeepEqual(got, want) {
		t.Fatalf("portCmp.Data = %#v, want %#v", got, want)
	}

	verdict, ok := rule.Exprs[4].(*expr.Verdict)
	if !ok {
		t.Fatalf("expr[4] = %T, want *expr.Verdict", rule.Exprs[4])
	}
	if got, want := verdict.Kind, expr.VerdictReturn; got != want {
		t.Fatalf("verdict.Kind = %v, want %v", got, want)
	}
}

func assertMarkRule(t *testing.T, rule *nftables.Rule, table *nftables.Table, chain *nftables.Chain, proto uint8, mark int) {
	t.Helper()
	assertRuleLocation(t, rule, table, chain)
	if got, want := len(rule.Exprs), 7; got != want {
		t.Fatalf("len(rule.Exprs) = %d, want %d", got, want)
	}

	ct, ok := rule.Exprs[0].(*expr.Ct)
	if !ok {
		t.Fatalf("expr[0] = %T, want *expr.Ct", rule.Exprs[0])
	}
	if got, want := ct.Key, expr.CtKeySTATE; got != want {
		t.Fatalf("ct.Key = %v, want %v", got, want)
	}

	bitwise, ok := rule.Exprs[1].(*expr.Bitwise)
	if !ok {
		t.Fatalf("expr[1] = %T, want *expr.Bitwise", rule.Exprs[1])
	}
	if got, want := bitwise.Mask, nftU32(expr.CtStateBitNEW); !reflect.DeepEqual(got, want) {
		t.Fatalf("bitwise.Mask = %#v, want %#v", got, want)
	}

	stateCmp, ok := rule.Exprs[2].(*expr.Cmp)
	if !ok {
		t.Fatalf("expr[2] = %T, want *expr.Cmp", rule.Exprs[2])
	}
	if got, want := stateCmp.Op, expr.CmpOpNeq; got != want {
		t.Fatalf("stateCmp.Op = %v, want %v", got, want)
	}
	if got, want := stateCmp.Data, nftU32(0); !reflect.DeepEqual(got, want) {
		t.Fatalf("stateCmp.Data = %#v, want %#v", got, want)
	}

	meta, ok := rule.Exprs[3].(*expr.Meta)
	if !ok {
		t.Fatalf("expr[3] = %T, want *expr.Meta", rule.Exprs[3])
	}
	if got, want := meta.Key, expr.MetaKeyL4PROTO; got != want {
		t.Fatalf("meta.Key = %v, want %v", got, want)
	}

	protoCmp, ok := rule.Exprs[4].(*expr.Cmp)
	if !ok {
		t.Fatalf("expr[4] = %T, want *expr.Cmp", rule.Exprs[4])
	}
	if got, want := protoCmp.Op, expr.CmpOpEq; got != want {
		t.Fatalf("protoCmp.Op = %v, want %v", got, want)
	}
	if got, want := protoCmp.Data, []byte{proto}; !reflect.DeepEqual(got, want) {
		t.Fatalf("protoCmp.Data = %#v, want %#v", got, want)
	}

	immediate, ok := rule.Exprs[5].(*expr.Immediate)
	if !ok {
		t.Fatalf("expr[5] = %T, want *expr.Immediate", rule.Exprs[5])
	}
	if got, want := immediate.Register, uint32(nftReg1); got != want {
		t.Fatalf("immediate.Register = %d, want %d", got, want)
	}
	if got, want := immediate.Data, nftU32(uint32(mark)); !reflect.DeepEqual(got, want) {
		t.Fatalf("immediate.Data = %#v, want %#v", got, want)
	}

	markMeta, ok := rule.Exprs[6].(*expr.Meta)
	if !ok {
		t.Fatalf("expr[6] = %T, want *expr.Meta", rule.Exprs[6])
	}
	if got, want := markMeta.Key, expr.MetaKeyMARK; got != want {
		t.Fatalf("markMeta.Key = %v, want %v", got, want)
	}
	if !markMeta.SourceRegister {
		t.Fatal("markMeta.SourceRegister = false, want true")
	}
	if got, want := markMeta.Register, uint32(nftReg1); got != want {
		t.Fatalf("markMeta.Register = %d, want %d", got, want)
	}
}

func assertRuleLocation(t *testing.T, rule *nftables.Rule, table *nftables.Table, chain *nftables.Chain) {
	t.Helper()
	if rule.Table != table {
		t.Fatalf("rule.Table = %#v, want added table pointer", rule.Table)
	}
	if rule.Chain != chain {
		t.Fatalf("rule.Chain = %#v, want added chain pointer", rule.Chain)
	}
}
