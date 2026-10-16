# mo's patches to mermaid-ascii

A copy of github.com/AlexanderGrooff/mermaid-ascii at v0.0.0-20260908213847-5f00e3d9ac9f,
used by mo through a `replace` in its go.mod. MIT licensed, as LICENSE says.

## Joined paths placed below all their parents

`pkg/graph/graph.go`, `createMapping`: mermaid-ascii puts each node one level past the first
parent that reaches it. When paths join, the joined node can then sit beside a deeper parent,
and that parent's arrow climbs back to it, crossing others and entering the box from its side.

When that happens, and only then, a node goes one level past its deepest parent instead: the
longest path from a root, the edges back to a node on the way left out so cycles end. Nodes
are taken by depth, so each is placed before its children; what is left unplaced is placed as
before. A layout whose arrows all go forward is kept as it was.

The golden cases this changes in `pkg/graph/testdata` were written again from what is drawn
now (`UPDATE_GOLDEN=1 go test ./pkg/graph`).
