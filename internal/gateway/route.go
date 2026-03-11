//go:build linux

package gateway

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"syscall"

	"github.com/vishvananda/netlink"
)

const (
	sluiceRouteTable   = defaultRouteTable
	sluiceRulePriority = defaultRulePriority
	sluiceMark         = defaultFwmark
	sluiceTUNAddress   = "10.0.85.1/24"
)

type routePolicy struct {
	routeTable   int
	rulePriority int
	fwmark       int
}

var defaultRoutePolicy = routePolicy{
	routeTable:   sluiceRouteTable,
	rulePriority: sluiceRulePriority,
	fwmark:       sluiceMark,
}

type netlinkAPI interface {
	LinkByName(name string) (netlink.Link, error)
	AddrAdd(link netlink.Link, addr *netlink.Addr) error
	LinkSetUp(link netlink.Link) error
	RouteGet(destination net.IP) ([]netlink.Route, error)
	RouteListFiltered(family int, filter *netlink.Route, filterMask uint64) ([]netlink.Route, error)
	RouteReplace(route *netlink.Route) error
	RouteDel(route *netlink.Route) error
	RuleList(family int) ([]netlink.Rule, error)
	RuleAdd(rule *netlink.Rule) error
	RuleDel(rule *netlink.Rule) error
}

type systemNetlink struct{}

func (systemNetlink) LinkByName(name string) (netlink.Link, error) {
	return netlink.LinkByName(name)
}

func (systemNetlink) AddrAdd(link netlink.Link, addr *netlink.Addr) error {
	return netlink.AddrAdd(link, addr)
}

func (systemNetlink) LinkSetUp(link netlink.Link) error {
	return netlink.LinkSetUp(link)
}

func (systemNetlink) RouteGet(destination net.IP) ([]netlink.Route, error) {
	return netlink.RouteGet(destination)
}

func (systemNetlink) RouteListFiltered(family int, filter *netlink.Route, filterMask uint64) ([]netlink.Route, error) {
	return netlink.RouteListFiltered(family, filter, filterMask)
}

func (systemNetlink) RouteReplace(route *netlink.Route) error {
	return netlink.RouteReplace(route)
}

func (systemNetlink) RouteDel(route *netlink.Route) error {
	return netlink.RouteDel(route)
}

func (systemNetlink) RuleList(family int) ([]netlink.Rule, error) {
	return netlink.RuleList(family)
}

func (systemNetlink) RuleAdd(rule *netlink.Rule) error {
	return netlink.RuleAdd(rule)
}

func (systemNetlink) RuleDel(rule *netlink.Rule) error {
	return netlink.RuleDel(rule)
}

type RouteManager struct {
	netlink netlinkAPI
	policy  routePolicy
}

func NewRouteManager() *RouteManager {
	return &RouteManager{netlink: systemNetlink{}, policy: defaultRoutePolicy}
}

func NewRouteManagerWithPolicy(routeTable, rulePriority, fwmark int) *RouteManager {
	return &RouteManager{
		netlink: systemNetlink{},
		policy: routePolicy{
			routeTable:   routeTable,
			rulePriority: rulePriority,
			fwmark:       fwmark,
		},
	}
}

func (m *RouteManager) Reconcile() error {
	if err := m.deleteOwnedRules(); err != nil {
		return fmt.Errorf("delete owned rules: %w", err)
	}

	if err := m.deleteOwnedRoutes(); err != nil {
		return fmt.Errorf("delete owned routes: %w", err)
	}

	return nil
}

func (m *RouteManager) Setup(tunName string, proxyIP net.IP) error {
	tunName = strings.TrimSpace(tunName)
	if tunName == "" {
		return errors.New("tun name is required")
	}

	proxyIPv4 := proxyIP.To4()
	if proxyIPv4 == nil {
		return errors.New("proxy IP must be IPv4")
	}

	if err := m.Reconcile(); err != nil {
		return fmt.Errorf("reconcile owned routing: %w", err)
	}

	tun, err := m.handle().LinkByName(tunName)
	if err != nil {
		return fmt.Errorf("lookup tun link %q: %w", tunName, err)
	}

	if err := m.setupInterface(tun); err != nil {
		return fmt.Errorf("setup tun interface %q: %w", tunName, err)
	}

	bypassRoute, err := m.buildProxyBypassRoute(proxyIPv4)
	if err != nil {
		return fmt.Errorf("build proxy bypass route: %w", err)
	}

	defaultRoute := &netlink.Route{
		Table:     m.policyValues().routeTable,
		LinkIndex: tun.Attrs().Index,
		Dst:       defaultIPv4Route(),
	}

	if err := m.handle().RouteReplace(bypassRoute); err != nil {
		return fmt.Errorf("install proxy bypass route: %w", err)
	}

	if err := m.handle().RouteReplace(defaultRoute); err != nil {
		return fmt.Errorf("install default route: %w", err)
	}

	rule := m.ownedRule()
	if err := m.handle().RuleAdd(rule); err != nil {
		return fmt.Errorf("install policy rule: %w", err)
	}

	return nil
}

func (m *RouteManager) Cleanup() error {
	return m.Reconcile()
}

func (m *RouteManager) setupInterface(link netlink.Link) error {
	addr, err := netlink.ParseAddr(sluiceTUNAddress)
	if err != nil {
		return fmt.Errorf("parse tun address: %w", err)
	}

	if err := m.handle().AddrAdd(link, addr); err != nil && !errors.Is(err, syscall.EEXIST) {
		return fmt.Errorf("add tun address: %w", err)
	}

	if err := m.handle().LinkSetUp(link); err != nil {
		return fmt.Errorf("bring tun link up: %w", err)
	}

	return nil
}

func (m *RouteManager) buildProxyBypassRoute(proxyIP net.IP) (*netlink.Route, error) {
	routes, err := m.handle().RouteGet(proxyIP)
	if err != nil {
		return nil, fmt.Errorf("resolve current proxy route: %w", err)
	}
	if len(routes) == 0 {
		return nil, errors.New("no existing route to proxy IP")
	}

	selected := routes[0]
	if selected.LinkIndex == 0 && len(selected.MultiPath) == 0 {
		return nil, errors.New("proxy route missing link information")
	}

	return &netlink.Route{
		Table:     m.policyValues().routeTable,
		Dst:       hostRoute(proxyIP),
		LinkIndex: selected.LinkIndex,
		Gw:        cloneIP(selected.Gw),
		Src:       cloneIP(selected.Src),
		Scope:     selected.Scope,
		MultiPath: cloneNextHops(selected.MultiPath),
	}, nil
}

func (m *RouteManager) deleteOwnedRoutes() error {
	routes, err := m.handle().RouteListFiltered(netlink.FAMILY_V4, &netlink.Route{Table: m.policyValues().routeTable}, netlink.RT_FILTER_TABLE)
	if err != nil {
		return err
	}

	for i := range routes {
		route := routes[i]
		if err := m.handle().RouteDel(&route); err != nil {
			return err
		}
	}

	return nil
}

func (m *RouteManager) deleteOwnedRules() error {
	rules, err := m.handle().RuleList(netlink.FAMILY_V4)
	if err != nil {
		return err
	}

	for i := range rules {
		rule := rules[i]
		if !m.isOwnedRule(rule) {
			continue
		}

		if err := m.handle().RuleDel(&rule); err != nil {
			return err
		}
	}

	return nil
}

func (m *RouteManager) handle() netlinkAPI {
	if m != nil && m.netlink != nil {
		return m.netlink
	}

	return systemNetlink{}
}

func (m *RouteManager) policyValues() routePolicy {
	if m == nil {
		return defaultRoutePolicy
	}
	if m.policy.routeTable == 0 && m.policy.rulePriority == 0 && m.policy.fwmark == 0 {
		return defaultRoutePolicy
	}

	return m.policy
}

func (m *RouteManager) ownedRule() *netlink.Rule {
	policy := m.policyValues()
	rule := netlink.NewRule()
	rule.Family = netlink.FAMILY_V4
	rule.Table = policy.routeTable
	rule.Priority = policy.rulePriority
	rule.Mark = uint32(policy.fwmark)
	mask := uint32(policy.fwmark)
	rule.Mask = &mask
	return rule
}

func (m *RouteManager) isOwnedRule(rule netlink.Rule) bool {
	policy := m.policyValues()
	return rule.Family == netlink.FAMILY_V4 &&
		rule.Table == policy.routeTable &&
		rule.Priority == policy.rulePriority &&
		rule.Mark == uint32(policy.fwmark) &&
		rule.Mask != nil && *rule.Mask == uint32(policy.fwmark)
}

func ownedRule() *netlink.Rule {
	return (&RouteManager{policy: defaultRoutePolicy}).ownedRule()
}

func isOwnedRule(rule netlink.Rule) bool {
	return (&RouteManager{policy: defaultRoutePolicy}).isOwnedRule(rule)
}

func hostRoute(ip net.IP) *net.IPNet {
	masked := ip.Mask(net.CIDRMask(32, 32))
	return &net.IPNet{IP: masked, Mask: net.CIDRMask(32, 32)}
}

func defaultIPv4Route() *net.IPNet {
	return &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)}
}

func cloneIP(ip net.IP) net.IP {
	if ip == nil {
		return nil
	}

	clone := make(net.IP, len(ip))
	copy(clone, ip)
	return clone
}

func cloneNextHops(nextHops []*netlink.NexthopInfo) []*netlink.NexthopInfo {
	if len(nextHops) == 0 {
		return nil
	}

	cloned := make([]*netlink.NexthopInfo, 0, len(nextHops))
	for _, nextHop := range nextHops {
		if nextHop == nil {
			cloned = append(cloned, nil)
			continue
		}

		copyHop := *nextHop
		copyHop.Gw = cloneIP(nextHop.Gw)
		cloned = append(cloned, &copyHop)
	}

	return cloned
}
