package simulator

import (
	"fmt"
	"sync"
	"time"

	"github.com/gopcua/opcua/id"
	"github.com/gopcua/opcua/server"
	"github.com/gopcua/opcua/server/attrs"
	"github.com/gopcua/opcua/ua"
)

// loadSpace serves the load-test tags from plain maps. The SDK's namespace
// resolves the reference type hierarchy again for every reference it browses,
// which made browsing 50000 tags take seconds that real servers do not spend.
type loadSpace struct {
	id       uint16
	count    int
	mu       sync.Mutex
	children map[string][]*ua.ReferenceDescription
	folders  map[string]string
	tags     map[string]*loadTag
}

type loadTag struct {
	name     string
	value    float64
	offline  bool
	writable bool
}

func newLoadSpace(tags int) *loadSpace { return &loadSpace{count: tags} }

func (ns *loadSpace) ref(parent, child, name string, kind uint32, class ua.NodeClass) {
	typeDefinition := uint32(id.BaseDataVariableType)
	if class == ua.NodeClassObject {
		typeDefinition = id.FolderType
	}
	ns.children[parent] = append(ns.children[parent], &ua.ReferenceDescription{
		ReferenceTypeID: ua.NewNumericNodeID(0, kind),
		IsForward:       true,
		NodeID:          ua.NewStringExpandedNodeID(ns.id, child),
		BrowseName:      &ua.QualifiedName{NamespaceIndex: ns.id, Name: name},
		DisplayName:     &ua.LocalizedText{EncodingMask: ua.LocalizedTextText, Text: name},
		NodeClass:       class,
		TypeDefinition:  ua.NewNumericExpandedNodeID(0, typeDefinition),
	})
}

// folderNode is the SDK node that links Objects to the load tree.
func (ns *loadSpace) folderNode() *server.Node {
	return server.NewNode(ua.NewStringNodeID(ns.id, "Load"), server.Attributes{
		ua.AttributeIDNodeClass:   server.DataValueFromValue(uint32(ua.NodeClassObject)),
		ua.AttributeIDBrowseName:  server.DataValueFromValue(&ua.QualifiedName{NamespaceIndex: ns.id, Name: "Load"}),
		ua.AttributeIDDisplayName: server.DataValueFromValue(attrs.DisplayName("Load", "")),
	}, nil, nil)
}

func (ns *loadSpace) Name() string                        { return "urn:opcua-ai-studio:load" }
func (ns *loadSpace) AddNode(n *server.Node) *server.Node { return n }
func (ns *loadSpace) Node(*ua.NodeID) *server.Node        { return nil }
func (ns *loadSpace) Objects() *server.Node               { return ns.folderNode() }
func (ns *loadSpace) Root() *server.Node                  { return ns.folderNode() }
func (ns *loadSpace) ID() uint16                          { return ns.id }

// SetID builds the tree once the server assigns the namespace index, which
// every NodeID carries.
func (ns *loadSpace) SetID(index uint16) {
	ns.id, ns.children, ns.folders, ns.tags = index, map[string][]*ua.ReferenceDescription{}, map[string]string{"Load": "Load"}, make(map[string]*loadTag, ns.count)
	for i := 0; i < ns.count; i++ {
		station := fmt.Sprintf("Station%03d", i/100)
		if i%100 == 0 {
			ns.folders["Load."+station] = station
			ns.ref("Load", "Load."+station, station, id.Organizes, ua.NodeClassObject)
		}
		name := fmt.Sprintf("Tag%02d", i%100)
		key := "Load." + station + "." + name
		ns.tags[key] = &loadTag{name: name, value: float64(i), offline: i%50 == 49, writable: i%10 == 0}
		ns.ref("Load."+station, key, name, id.HasComponent, ua.NodeClassVariable)
	}
}

func (ns *loadSpace) Browse(bd *ua.BrowseDescription) *ua.BrowseResult {
	key := bd.NodeID.StringID()
	_, isFolder := ns.folders[key]
	if _, isTag := ns.tags[key]; !isFolder && !isTag {
		return &ua.BrowseResult{StatusCode: ua.StatusBadNodeIDUnknown}
	}
	refs := []*ua.ReferenceDescription{}
	if bd.BrowseDirection != ua.BrowseDirectionInverse {
		for _, r := range ns.children[key] {
			if bd.NodeClassMask == 0 || bd.NodeClassMask&uint32(r.NodeClass) != 0 {
				refs = append(refs, r)
			}
		}
	}
	return &ua.BrowseResult{StatusCode: ua.StatusGood, References: refs}
}

func status(code ua.StatusCode) *ua.DataValue {
	return &ua.DataValue{EncodingMask: ua.DataValueStatusCode | ua.DataValueServerTimestamp, Status: code, ServerTimestamp: time.Now()}
}

func value(v any) *ua.DataValue {
	return &ua.DataValue{EncodingMask: ua.DataValueValue | ua.DataValueServerTimestamp, Value: ua.MustVariant(v), ServerTimestamp: time.Now()}
}

func (ns *loadSpace) Attribute(n *ua.NodeID, a ua.AttributeID) *ua.DataValue {
	key := n.StringID()
	if name, ok := ns.folders[key]; ok {
		switch a {
		case ua.AttributeIDNodeClass:
			return value(int32(ua.NodeClassObject))
		case ua.AttributeIDBrowseName:
			return value(&ua.QualifiedName{NamespaceIndex: ns.id, Name: name})
		case ua.AttributeIDDisplayName:
			return value(attrs.DisplayName(name, ""))
		}
		return status(ua.StatusBadAttributeIDInvalid)
	}
	t, ok := ns.tags[key]
	if !ok {
		return status(ua.StatusBadNodeIDUnknown)
	}
	switch a {
	case ua.AttributeIDNodeClass:
		return value(int32(ua.NodeClassVariable))
	case ua.AttributeIDBrowseName:
		return value(&ua.QualifiedName{NamespaceIndex: ns.id, Name: t.name})
	case ua.AttributeIDDisplayName:
		return value(attrs.DisplayName(t.name, ""))
	case ua.AttributeIDDataType:
		return value(ua.NewNumericNodeID(0, id.Double))
	case ua.AttributeIDAccessLevel, ua.AttributeIDUserAccessLevel:
		level := uint8(ua.AccessLevelTypeCurrentRead)
		if t.writable {
			level |= uint8(ua.AccessLevelTypeCurrentWrite)
		}
		return value(level)
	case ua.AttributeIDValue:
		if t.offline {
			return status(ua.StatusBadNoCommunication)
		}
		ns.mu.Lock()
		defer ns.mu.Unlock()
		dv := value(t.value)
		dv.EncodingMask |= ua.DataValueSourceTimestamp
		dv.SourceTimestamp = dv.ServerTimestamp
		return dv
	}
	return status(ua.StatusBadAttributeIDInvalid)
}

func (ns *loadSpace) SetAttribute(n *ua.NodeID, a ua.AttributeID, v *ua.DataValue) ua.StatusCode {
	t, ok := ns.tags[n.StringID()]
	switch {
	case !ok:
		return ua.StatusBadNodeIDUnknown
	case a != ua.AttributeIDValue || !t.writable:
		return ua.StatusBadNotWritable
	case v == nil || v.Value == nil:
		return ua.StatusBadTypeMismatch
	}
	f, ok := v.Value.Value().(float64)
	if !ok {
		return ua.StatusBadTypeMismatch
	}
	ns.mu.Lock()
	t.value = f
	ns.mu.Unlock()
	return ua.StatusOK
}
