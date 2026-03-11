//go:build linux

package gateway

import (
	"errors"
	"net"
	"reflect"
	"syscall"
	"testing"

	"github.com/vishvananda/netlink"
)

func TestRouteManagerSetup(t *testing.T) {
	t.Parallel()

	fake := &fakeNetlink{
		links: map[string]netlink.Link{
			"sluice0": &netlink.Device{LinkAttrs: netlink.LinkAttrs{Name: "sluice0", Index: 42}},
		},
		routeGetRoutes: []netlink.Route{{
			LinkIndex: 7,
			Gw:        net.ParseIP("192.0.2.1").To4(),
			Src:       net.ParseIP("192.0.2.25").To4(),
			Scope:     netlink.SCOPE_UNIVERSE,
		}},
		routes: []netlink.Route{{Table: sluiceRouteTable, LinkIndex: 99}},
		rules:  []netlink.Rule{ownedRuleValue()},
	}

	mgr := &RouteManager{netlink: fake}
	proxyIP := net.ParseIP("198.51.100.10")

	if err := mgr.Setup("sluice0", proxyIP); err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	if len(fake.deletedRules) != 1 {
		t.Fatalf("deletedRules = %d, want 1", len(fake.deletedRules))
	}
	if len(fake.deletedRoutes) != 1 {
		t.Fatalf("deletedRoutes = %d, want 1", len(fake.deletedRoutes))
	}
	if len(fake.addedAddrs) != 1 {
		t.Fatalf("addedAddrs = %d, want 1", len(fake.addedAddrs))
	}
	if got, want := fake.addedAddrs[0].String(), sluiceTUNAddress; got != want {
		t.Fatalf("addedAddrs[0] = %q, want %q", got, want)
	}
	if len(fake.linkSetUpNames) != 1 {
		t.Fatalf("linkSetUpNames = %d, want 1", len(fake.linkSetUpNames))
	}
	if got, want := fake.linkSetUpNames[0], "sluice0"; got != want {
		t.Fatalf("linkSetUpNames[0] = %q, want %q", got, want)
	}
	if len(fake.replacedRoutes) != 2 {
		t.Fatalf("replacedRoutes = %d, want 2", len(fake.replacedRoutes))
	}

	bypass := fake.replacedRoutes[0]
	if got, want := bypass.Table, sluiceRouteTable; got != want {
		t.Fatalf("bypass.Table = %d, want %d", got, want)
	}
	if got, want := bypass.LinkIndex, 7; got != want {
		t.Fatalf("bypass.LinkIndex = %d, want %d", got, want)
	}
	if got, want := bypass.Gw.String(), "192.0.2.1"; got != want {
		t.Fatalf("bypass.Gw = %s, want %s", got, want)
	}
	if !reflect.DeepEqual(bypass.Dst, hostRoute(proxyIP.To4())) {
		t.Fatalf("bypass.Dst = %#v, want %#v", bypass.Dst, hostRoute(proxyIP.To4()))
	}

	defaultRoute := fake.replacedRoutes[1]
	if got, want := defaultRoute.Table, sluiceRouteTable; got != want {
		t.Fatalf("defaultRoute.Table = %d, want %d", got, want)
	}
	if got, want := defaultRoute.LinkIndex, 42; got != want {
		t.Fatalf("defaultRoute.LinkIndex = %d, want %d", got, want)
	}
	if got, want := defaultRoute.Dst.String(), defaultIPv4Route().String(); got != want {
		t.Fatalf("defaultRoute.Dst = %q, want %q", got, want)
	}

	if len(fake.addedRules) != 1 {
		t.Fatalf("addedRules = %d, want 1", len(fake.addedRules))
	}
	if got := fake.addedRules[0]; !isOwnedRule(got) {
		t.Fatalf("added rule = %#v, want owned rule", got)
	}
	if len(fake.routeGetDestinations) != 1 || !fake.routeGetDestinations[0].Equal(proxyIP.To4()) {
		t.Fatalf("RouteGet destinations = %#v, want [%s]", fake.routeGetDestinations, proxyIP.To4())
	}
}

func TestRouteManagerCleanupIsIdempotent(t *testing.T) {
	t.Parallel()

	fake := &fakeNetlink{
		routes: []netlink.Route{{Table: sluiceRouteTable, LinkIndex: 5}},
		rules:  []netlink.Rule{ownedRuleValue()},
	}

	mgr := &RouteManager{netlink: fake}

	if err := mgr.Cleanup(); err != nil {
		t.Fatalf("Cleanup() first call error = %v", err)
	}
	if err := mgr.Cleanup(); err != nil {
		t.Fatalf("Cleanup() second call error = %v", err)
	}

	if got, want := len(fake.deletedRules), 1; got != want {
		t.Fatalf("deletedRules = %d, want %d", got, want)
	}
	if got, want := len(fake.deletedRoutes), 1; got != want {
		t.Fatalf("deletedRoutes = %d, want %d", got, want)
	}
}

func TestRouteManagerSetupRejectsNonIPv4(t *testing.T) {
	t.Parallel()

	mgr := &RouteManager{netlink: &fakeNetlink{}}
	if err := mgr.Setup("sluice0", net.ParseIP("2001:db8::1")); err == nil {
		t.Fatal("Setup() error = nil, want error")
	}
}

func TestRouteManagerSetupAllowsExistingTUNAddress(t *testing.T) {
	t.Parallel()

	fake := &fakeNetlink{
		links: map[string]netlink.Link{
			"sluice0": &netlink.Device{LinkAttrs: netlink.LinkAttrs{Name: "sluice0", Index: 42}},
		},
		addrAddErr: syscall.EEXIST,
		routeGetRoutes: []netlink.Route{{
			LinkIndex: 7,
			Gw:        net.ParseIP("192.0.2.1").To4(),
		}},
	}

	mgr := &RouteManager{netlink: fake}
	if err := mgr.Setup("sluice0", net.ParseIP("198.51.100.10")); err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	if len(fake.linkSetUpNames) != 1 {
		t.Fatalf("linkSetUpNames = %d, want 1", len(fake.linkSetUpNames))
	}
	if len(fake.replacedRoutes) != 2 {
		t.Fatalf("replacedRoutes = %d, want 2", len(fake.replacedRoutes))
	}
}

func TestRouteManagerSetupRequiresProxyRoute(t *testing.T) {
	t.Parallel()

	fake := &fakeNetlink{
		links: map[string]netlink.Link{
			"sluice0": &netlink.Device{LinkAttrs: netlink.LinkAttrs{Name: "sluice0", Index: 42}},
		},
		routeGetErr: errors.New("lookup failed"),
	}

	mgr := &RouteManager{netlink: fake}
	if err := mgr.Setup("sluice0", net.ParseIP("198.51.100.10")); err == nil {
		t.Fatal("Setup() error = nil, want error")
	}
}

func TestRouteManagerSetupUsesConfiguredPolicy(t *testing.T) {
	t.Parallel()

	const (
		routeTable   = 201
		rulePriority = 22222
		fwmark       = 0x9
	)

	fwmarkMask := uint32(fwmark)
	customOwnedRule := netlink.Rule{
		Family:   netlink.FAMILY_V4,
		Table:    routeTable,
		Priority: rulePriority,
		Mark:     fwmark,
		Mask:     &fwmarkMask,
	}

	fake := &fakeNetlink{
		links: map[string]netlink.Link{
			"sluice0": &netlink.Device{LinkAttrs: netlink.LinkAttrs{Name: "sluice0", Index: 42}},
		},
		routeGetRoutes: []netlink.Route{{
			LinkIndex: 7,
			Gw:        net.ParseIP("192.0.2.1").To4(),
		}},
		routes: []netlink.Route{
			{Table: routeTable, LinkIndex: 5},
			{Table: sluiceRouteTable, LinkIndex: 99},
		},
		rules: []netlink.Rule{customOwnedRule, ownedRuleValue()},
	}

	mgr := NewRouteManagerWithPolicy(routeTable, rulePriority, fwmark)
	mgr.netlink = fake

	if err := mgr.Setup("sluice0", net.ParseIP("198.51.100.10")); err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	if got, want := len(fake.deletedRoutes), 1; got != want {
		t.Fatalf("deletedRoutes = %d, want %d", got, want)
	}
	if got, want := fake.deletedRoutes[0].Table, routeTable; got != want {
		t.Fatalf("deletedRoutes[0].Table = %d, want %d", got, want)
	}

	if got, want := len(fake.deletedRules), 1; got != want {
		t.Fatalf("deletedRules = %d, want %d", got, want)
	}
	if got, want := fake.deletedRules[0].Table, routeTable; got != want {
		t.Fatalf("deletedRules[0].Table = %d, want %d", got, want)
	}

	if got, want := len(fake.replacedRoutes), 2; got != want {
		t.Fatalf("replacedRoutes = %d, want %d", got, want)
	}
	for i, replaced := range fake.replacedRoutes {
		if got, want := replaced.Table, routeTable; got != want {
			t.Fatalf("replacedRoutes[%d].Table = %d, want %d", i, got, want)
		}
	}

	if got, want := len(fake.addedRules), 1; got != want {
		t.Fatalf("addedRules = %d, want %d", got, want)
	}
	addedRule := fake.addedRules[0]
	if got, want := addedRule.Table, routeTable; got != want {
		t.Fatalf("addedRule.Table = %d, want %d", got, want)
	}
	if got, want := addedRule.Priority, rulePriority; got != want {
		t.Fatalf("addedRule.Priority = %d, want %d", got, want)
	}
	if got, want := addedRule.Mark, uint32(fwmark); got != want {
		t.Fatalf("addedRule.Mark = %d, want %d", got, want)
	}
	if addedRule.Mask == nil || *addedRule.Mask != uint32(fwmark) {
		t.Fatalf("addedRule.Mask = %#v, want %d", addedRule.Mask, fwmark)
	}
}

type fakeNetlink struct {
	links                map[string]netlink.Link
	addrAddErr           error
	addedAddrs           []netlink.Addr
	linkSetUpNames       []string
	routeGetRoutes       []netlink.Route
	routeGetErr          error
	routeGetDestinations []net.IP
	routes               []netlink.Route
	deletedRoutes        []netlink.Route
	replacedRoutes       []netlink.Route
	rules                []netlink.Rule
	deletedRules         []netlink.Rule
	addedRules           []netlink.Rule
}

func ownedRuleValue() netlink.Rule {
	return *ownedRule()
}

func (f *fakeNetlink) LinkByName(name string) (netlink.Link, error) {
	link, ok := f.links[name]
	if !ok {
		return nil, errors.New("link not found")
	}
	return link, nil
}

func (f *fakeNetlink) AddrAdd(link netlink.Link, addr *netlink.Addr) error {
	if addr != nil {
		f.addedAddrs = append(f.addedAddrs, *addr)
	}
	return f.addrAddErr
}

func (f *fakeNetlink) LinkSetUp(link netlink.Link) error {
	f.linkSetUpNames = append(f.linkSetUpNames, link.Attrs().Name)
	return nil
}

func (f *fakeNetlink) RouteGet(destination net.IP) ([]netlink.Route, error) {
	f.routeGetDestinations = append(f.routeGetDestinations, cloneIP(destination))
	if f.routeGetErr != nil {
		return nil, f.routeGetErr
	}
	return append([]netlink.Route(nil), f.routeGetRoutes...), nil
}

func (f *fakeNetlink) RouteListFiltered(_ int, filter *netlink.Route, _ uint64) ([]netlink.Route, error) {
	var routes []netlink.Route
	for _, route := range f.routes {
		if filter != nil && filter.Table != 0 && route.Table != filter.Table {
			continue
		}
		routes = append(routes, route)
	}
	return routes, nil
}

func (f *fakeNetlink) RouteReplace(route *netlink.Route) error {
	f.replacedRoutes = append(f.replacedRoutes, *route)
	return nil
}

func (f *fakeNetlink) RouteDel(route *netlink.Route) error {
	f.deletedRoutes = append(f.deletedRoutes, *route)
	filtered := f.routes[:0]
	for _, existing := range f.routes {
		if reflect.DeepEqual(existing, *route) {
			continue
		}
		filtered = append(filtered, existing)
	}
	f.routes = filtered
	return nil
}

func (f *fakeNetlink) RuleList(_ int) ([]netlink.Rule, error) {
	return append([]netlink.Rule(nil), f.rules...), nil
}

func (f *fakeNetlink) RuleAdd(rule *netlink.Rule) error {
	f.addedRules = append(f.addedRules, *rule)
	return nil
}

func (f *fakeNetlink) RuleDel(rule *netlink.Rule) error {
	f.deletedRules = append(f.deletedRules, *rule)
	filtered := f.rules[:0]
	for _, existing := range f.rules {
		if reflect.DeepEqual(existing, *rule) {
			continue
		}
		filtered = append(filtered, existing)
	}
	f.rules = filtered
	return nil
}
