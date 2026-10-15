package kubernetes

import (
	"cmp"
	"slices"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const kindListenerSet = "ListenerSet"

// ListenerSetStatus is the status of a ListenerSet attached to a served
// gateway.
type ListenerSetStatus struct {
	Generation int64
	Conditions []metav1.Condition
	Listeners  []gwv1.ListenerEntryStatus
}

// setInfo is a ListenerSet referencing a served gateway.
type setInfo struct {
	set       *gwv1.ListenerSet
	gw        nsName
	allowed   bool
	listeners []*listener
}

// listenerSetParent returns the gateway a ListenerSet references, false for
// parents that are not Gateways.
func listenerSetParent(ls *gwv1.ListenerSet) (nsName, bool) {
	ref := ls.Spec.ParentRef
	if ptrOr(ref.Group, gwv1.Group(groupGateway)) != groupGateway || ptrOr(ref.Kind, kindGateway) != kindGateway {
		return nsName{}, false
	}

	return nsName{Namespace: string(ptrOr(ref.Namespace, gwv1.Namespace(ls.Namespace))), Name: string(ref.Name)}, true
}

// listenerSetAllowed reports whether allowedListeners of gw admits ls; no
// ListenerSet is allowed by default.
func listenerSetAllowed(gw *gwv1.Gateway, ls *gwv1.ListenerSet, idx *index) bool {
	if gw.Spec.AllowedListeners == nil || gw.Spec.AllowedListeners.Namespaces == nil {
		return false
	}

	ns := gw.Spec.AllowedListeners.Namespaces

	switch ptrOr(ns.From, gwv1.NamespacesFromNone) {
	case gwv1.NamespacesFromAll:
		return true
	case gwv1.NamespacesFromSame:
		return ls.Namespace == gw.Namespace
	case gwv1.NamespacesFromSelector:
		if ns.Selector == nil {
			return false
		}

		sel, err := metav1.LabelSelectorAsSelector(ns.Selector)
		if err != nil {
			return false
		}

		return sel.Matches(labels.Set(idx.namespaceLabels(ls.Namespace)))
	default:
		return false
	}
}

// listenerSetsOf returns the ListenerSets referencing gw in listener
// precedence order: oldest first, then by namespace/name.
func listenerSetsOf(gw *gwv1.Gateway, idx *index) []*gwv1.ListenerSet {
	var out []*gwv1.ListenerSet

	for _, ls := range idx.listenerSets {
		if parent, ok := listenerSetParent(ls); ok && parent == key(gw) {
			out = append(out, ls)
		}
	}

	slices.SortFunc(out, func(a, b *gwv1.ListenerSet) int {
		if c := a.CreationTimestamp.Compare(b.CreationTimestamp.Time); c != 0 {
			return c
		}

		return cmp.Compare(key(a).String(), key(b).String())
	})

	return out
}

// attachedListenerSets returns the ListenerSets of gw its allowedListeners
// admits, in precedence order.
func attachedListenerSets(gw *gwv1.Gateway, idx *index) []*gwv1.ListenerSet {
	var out []*gwv1.ListenerSet

	for _, ls := range listenerSetsOf(gw, idx) {
		if listenerSetAllowed(gw, ls, idx) {
			out = append(out, ls)
		}
	}

	return out
}

// parentListeners returns the gateway and listeners a parentRef of a route
// in namespace ns selects: the listeners defined by the gateway itself or by
// the ListenerSet. ok is false for parents not served by this controller;
// err explains why a served ListenerSet has no listeners.
func (t *translator) parentListeners(ref gwv1.ParentReference, ns string, gateways map[nsName][]*listener) (gw nsName, lis []*listener, err string, ok bool) {
	if ptrOr(ref.Group, gwv1.Group(groupGateway)) != groupGateway {
		return nsName{}, nil, "", false
	}

	k := nsName{Namespace: string(ptrOr(ref.Namespace, gwv1.Namespace(ns))), Name: string(ref.Name)}

	switch ptrOr(ref.Kind, kindGateway) {
	case kindGateway:
		all, ok := gateways[k]
		if !ok {
			return nsName{}, nil, "", false
		}

		for _, li := range all {
			if li.set == nil {
				lis = append(lis, li)
			}
		}

		return k, lis, "", true
	case kindListenerSet:
		info, ok := t.sets[k]
		if !ok {
			return nsName{}, nil, "", false
		}

		if !info.allowed {
			return info.gw, nil, "the ListenerSet is not allowed by its gateway", true
		}

		return info.gw, info.listeners, "", true
	default:
		return nsName{}, nil, "", false
	}
}

// listenerSets adds the listeners of the ListenerSets admitted by gw after
// the listeners of gw.
func (t *translator) listenerSets(gw *gwv1.Gateway, lis []*listener) []*listener {
	for _, ls := range listenerSetsOf(gw, t.idx) {
		info := &setInfo{set: ls, gw: key(gw), allowed: listenerSetAllowed(gw, ls, t.idx)}
		t.sets[key(ls)] = info

		if !info.allowed {
			continue
		}

		for _, e := range ls.Spec.Listeners {
			li := t.listener(gw, ls, gwv1.Listener(e))
			info.listeners = append(info.listeners, li)
			lis = append(lis, li)
		}
	}

	return lis
}

// finishListenerSets reports the status of ListenerSets and counts the
// attached ones on their gateways.
func (t *translator) finishListenerSets() {
	for k, info := range t.sets {
		ls := info.set
		gen := ls.Generation
		st := &ListenerSetStatus{Generation: gen}

		gwStatus := t.out.Gateways[info.gw]
		parentAccepted := gwStatus != nil && meta.IsStatusConditionTrue(gwStatus.Conditions, string(gwv1.GatewayConditionAccepted))

		setConditions := func(accepted bool, acceptedReason string, programmed bool, programmedReason, msg string) {
			st.Conditions = []metav1.Condition{
				t.condition(gen, string(gwv1.ListenerSetConditionAccepted), accepted, acceptedReason, msg),
				t.condition(gen, string(gwv1.ListenerSetConditionProgrammed), programmed, programmedReason, msg),
			}
		}

		switch {
		case !info.allowed:
			reason := string(gwv1.ListenerSetReasonNotAllowed)
			setConditions(false, reason, false, reason, "the parent gateway does not allow this ListenerSet")
		case !parentAccepted:
			reason := string(gwv1.ListenerSetReasonParentNotAccepted)
			setConditions(false, reason, false, reason, "the parent gateway is not accepted")
		default:
			valid := 0
			for _, li := range info.listeners {
				if li.valid {
					valid++
				}
			}

			switch {
			case valid == 0:
				reason := string(gwv1.ListenerSetReasonListenersNotValid)
				setConditions(false, reason, false, reason, "no listener is valid")
			case valid < len(info.listeners):
				setConditions(true, string(gwv1.ListenerSetReasonListenersNotValid), true, string(gwv1.ListenerSetReasonProgrammed), "some listeners are not valid")
			default:
				setConditions(true, string(gwv1.ListenerSetReasonAccepted), true, string(gwv1.ListenerSetReasonProgrammed), "")
			}
		}

		if info.allowed {
			for _, li := range info.listeners {
				t.finishListener(li)
				st.Listeners = append(st.Listeners, gwv1.ListenerEntryStatus(*li.status))
			}
		}

		if gwStatus != nil && gwStatus.AttachedListenerSets != nil && meta.IsStatusConditionTrue(st.Conditions, string(gwv1.ListenerSetConditionAccepted)) {
			*gwStatus.AttachedListenerSets++
		}

		t.out.ListenerSets[k] = st
	}
}
