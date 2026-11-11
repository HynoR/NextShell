package ui

import "strings"

// treeBuild is a tree being built (TreeBase): its items in the order
// built, the items whose Group is being built, innermost last, and a key
// pressed on an item that moves the focus, which finish applies once all
// the items are built.
type treeBuild struct {
	c       *context
	items   []treeEntry
	parents []int
	move    treeMove
	from    int
}

// treeEntry is an item of a tree, and the index of the item it is in, -1
// at the top.
type treeEntry struct {
	n      *node
	parent int
}

// treeMove is where a key moves the focus from an item of a tree.
type treeMove uint8

const (
	treeStay treeMove = iota
	treeNext
	treePrev
	treeFirst
	treeLast
	treeChild
	treeParent
)

// TreeParts are the parts of a tree without a look: TreeBase makes it,
// and Item its items.
type treeParts struct {
	// Root holds the items: build them in its children with Item.
	Root *node
	c    *context
	b    *treeBuild
}

// TreeBase creates a tree without a look, as the web's role="tree": a
// column of items that Item makes, each a row the caller styles and fills,
// built inside the item it belongs to with Group. The tree is one stop of
// Tab, which goes to the item that had the focus last, or the one
// Selected; while an item has the focus, Up and Down move it to the items
// around, Home and End to the first and the last, Right opens the item or
// moves to its first child, Left closes it or moves to its parent (the
// other way round right to left), and typing the first letters of an
// item moves to it. Build the items in Root:
//
//	tree := ui.TreeBase(c)
//	tree.Root.Gap(2).Children(func() {
//		var node func(n *Node)
//		node = func(n *Node) {
//			c.Key(n.path) // the focus follows it as items above open
//			item := tree.Item(n.open) // nil for a leaf
//			item.Row.Gap(4).Padding(4, 8).PaddingStart(8 + 16*float32(item.Depth())).Radius(6)
//			if item.Row.FocusVisible() {
//				item.Row.Background(gray)
//			}
//			item.Row.Children(func() {
//				arrow := ui.Box(c).Size(16, 16)
//				if n.open != nil && arrow.Clicked() {
//					*n.open = !*n.open
//				}
//				ui.Text(c, n.name)
//			})
//			item.Group(func() {
//				for _, k := range n.children {
//					node(k)
//				}
//			})
//		}
//		for _, n := range app.roots {
//			node(n)
//		}
//	})
//
// Tree is TreeBase with the theme's look. TreeBase builds every item
// that shows; for trees of thousands of items, use Outline, which builds
// only the rows in view, with a Prefix of your own.
func coreTreeBase(c *context) treeParts {
	root := coreColumn(c).Role(RoleTree).FocusGroup(Vertical)
	root.widget = "Tree"
	b := &treeBuild{c: c}
	c.onBuilt(b.finish)
	return treeParts{Root: root, c: c, b: b}
}

// TreeItemParts are the parts of an item of a tree without a look: Item
// makes its Row, and Group the items inside it.
type treeItemParts struct {
	// Row is the item: a row that takes the focus, which assistive
	// technology sees as an item of a tree, with its level, open or
	// closed when it has children. It reports Clicked for the pointer,
	// Enter and Space. Style it and give it children.
	Row   *node
	c     *context
	b     *treeBuild
	open  *bool
	at    int
	depth int
}

// Item creates an item of the tree, at the top or, inside Group, in the
// item that Group is of. With a nil open it is a leaf; otherwise the items
// Group builds inside it show while *open is true, which Right and Left
// set, as does assistive technology. A key given with Context.Key just
// before keeps its focus on it, and on the items inside it, as items
// above open and close.
func (p treeParts) Item(open *bool) treeItemParts { return treeItem(p.c, p.b, open) }

func treeItem(c *context, b *treeBuild, open *bool) treeItemParts {
	item := coreRow(c).Focusable().Shrink(0).Role(RoleTreeItem)
	item.widget = "TreeItem"
	item.flags |= flagClickable | flagHover | flagTypeSelect
	parent, depth := -1, len(b.parents)
	if depth > 0 {
		parent = b.parents[depth-1]
	}
	at := len(b.items)
	b.items = append(b.items, treeEntry{n: item, parent: parent})
	branch := open != nil
	if s := item.st; branch && s.expand != 0 {
		// Assistive technology opening or closing it.
		*open = s.expand > 0
		s.expand = 0
		c.rt.consumed = true
	}
	press := func(m treeMove) { b.move, b.from = m, at }
	// Right opens, Left closes, the other way right to left.
	back, forth := item.arrows()
	switch {
	case item.Shortcut(0, KeyDown):
		press(treeNext)
	case item.Shortcut(0, KeyUp):
		press(treePrev)
	case item.Shortcut(0, KeyHome):
		press(treeFirst)
	case item.Shortcut(0, KeyEnd):
		press(treeLast)
	case item.Shortcut(0, forth):
		if branch && !*open {
			*open = true
			c.rt.consumed = true
		} else if branch {
			press(treeChild)
		}
	case item.Shortcut(0, back):
		if branch && *open {
			*open = false
			c.rt.consumed = true
		} else {
			press(treeParent)
		}
	}
	item.level, item.expandable, item.expanded = depth+1, branch, branch && *open
	item.setPos = at + 1
	return treeItemParts{Row: item, c: c, b: b, open: open, at: at, depth: depth}
}

// Depth returns how deep the item is: 0 at the top, 1 inside an item at
// the top, and so on, to indent its row by.
func (p treeItemParts) Depth() int { return p.depth }

// Group builds the items inside the item, which fn makes with the tree's
// Item, while it is open, in a column below the item's row. It returns the
// column, to style, as with a line along it, and nil while the item is
// closed or a leaf.
func (p treeItemParts) Group(fn func()) *node {
	open := p.open != nil && *p.open
	p.Row.expanded = open
	if !open {
		return nil
	}
	c, b := p.c, p.b
	g := coreColumn(c).Shrink(0)
	// Keyed by its item, so that the items inside keep theirs as items
	// above open and close.
	c.rekey(g, p.Row.id)
	b.parents = append(b.parents, p.at)
	g.Children(fn)
	b.parents = b.parents[:len(b.parents)-1]
	return g
}

// finish tells assistive technology how many items the tree shows, and
// moves the focus as the keys pressed on an item ask: to the items around
// it, its first child or its parent, or, for the first letters of an item
// typed, the item after it starting with them, round to it.
func (b *treeBuild) finish() {
	n := len(b.items)
	for _, it := range b.items {
		it.n.setSize = n
	}
	rt := b.c.rt
	from, to := -1, -1
	at := b.from
	switch b.move {
	case treeNext:
		to = min(at+1, n-1)
	case treePrev:
		to = max(at-1, 0)
	case treeFirst:
		to = 0
	case treeLast:
		to = n - 1
	case treeChild:
		if at+1 < n && b.items[at+1].parent == at {
			to = at + 1
		}
	case treeParent:
		to = b.items[at].parent
	}
	if b.move != treeStay {
		from = at
	}
	if s := rt.states[rt.focused]; s != nil && s.typing && s.typed != "" {
		for i, it := range b.items {
			if it.n.id != rt.focused {
				continue
			}
			// From the item after the one focused, round to it, as the
			// same letter again does, as a list's rows: more letters
			// start from the item focused.
			typed, start := strings.ToLower(s.typed), i
			if r := []rune(typed); strings.Count(typed, string(r[0])) == len(r) {
				typed, start = string(r[0]), i+1
			}
			for k := range n {
				if j := (start + k) % n; strings.HasPrefix(strings.ToLower(menuItemName(b.items[j].n)), typed) {
					from, to = i, j
					// The letters typed next add to these.
					next := b.items[j].n.st
					next.typed, next.typedAt = s.typed, s.typedAt
					break
				}
			}
			break
		}
	}
	if to < 0 || to == from {
		return
	}
	if id := b.items[to].n.id; rt.states[id] != nil {
		rt.focused, rt.focusVisible = id, true
		rt.reveal(id)
		rt.consumed = true
	}
}

// Tree creates a tree, whose items TreeItem builds in fn. While an item
// has the keyboard focus, Up and Down move it to the items around, Home
// and End to the first and the last, Right opens the item or moves to its
// first child, Left closes it or moves to its parent, and typing the first
// letters of an item moves to it. The tree is one stop of Tab.
//
//	ui.Tree(c, func() {
//		ui.TreeItem(c, "src", &app.srcOpen, func() {
//			ui.TreeItem(c, "main.go", nil, nil)
//		})
//		ui.TreeItem(c, "go.mod", nil, nil)
//	})
//
// Tree is TreeBase with the theme's look.
func coreTree(c *context, fn func()) *node {
	p := coreTreeBase(c)
	saved := c.tree
	c.tree = p.b
	p.Root.Children(fn)
	c.tree = saved
	return p.Root
}

// TreeItem creates an item of a Tree showing label, inside the item it is
// built in. children builds the items inside it, which show while *open
// is true; nil children, or a nil open, makes a leaf. A click on its arrow
// opens or closes it; Clicked reports a click elsewhere on it, or Enter,
// for choosing it, which Selected shows.
func coreTreeItem(c *context, label string, open *bool, children func()) *node {
	t := c.theme
	b := c.tree
	if b == nil {
		b = &treeBuild{c: c}
	}
	branch := children != nil && open != nil
	if !branch {
		open = nil
	}
	p := treeItem(c, b, open)
	item := p.Row.Height(t.Space(7)).Gap(t.Space(0.5)).AlignItems(Center).Padding(0, t.Space(2)).PaddingStart(t.Space(1 + 4*float32(p.depth)))
	item.flags |= flagOwnRing
	expanded := branch && *open
	item.styleFn = func(item *node) {
		if item.checked != 2 && item.Hovered() {
			item.bg = t.SurfaceHover
		}
	}
	item.DrawOver(func(p *Painter, r Rect) {
		if item.FocusVisible() {
			p.focusRingOf(item, r, [4]float32{t.Radius, t.Radius, t.Radius, t.Radius})
		}
	})
	item.Radius(t.Radius)
	item.Children(func() {
		arrow := coreBox(c).Size(t.Space(4), t.Space(4)).Shrink(0).Role(RoleNone)
		if branch {
			arrow.flags |= flagClickable
			if arrow.Clicked() {
				*open = !*open
				c.rt.consumed = true
			}
			rtl := arrow.rtl()
			arrow.Draw(func(p *Painter, r Rect) {
				var path Path
				switch {
				case expanded:
					path.MoveTo(r.X+r.W/4, r.Y+r.H*3/8).LineTo(r.X+r.W/2, r.Y+r.H*5/8).LineTo(r.X+r.W*3/4, r.Y+r.H*3/8)
				case rtl:
					path.MoveTo(r.X+r.W*5/8, r.Y+r.H/4).LineTo(r.X+r.W*3/8, r.Y+r.H/2).LineTo(r.X+r.W*5/8, r.Y+r.H*3/4)
				default:
					path.MoveTo(r.X+r.W*3/8, r.Y+r.H/4).LineTo(r.X+r.W*5/8, r.Y+r.H/2).LineTo(r.X+r.W*3/8, r.Y+r.H*3/4)
				}
				p.StrokePath(&path, 1.5, arrowColor(t, item))
			})
		}
		coreText(c, label).SingleLine()
	})
	if branch {
		p.Group(children)
	}
	return item
}
