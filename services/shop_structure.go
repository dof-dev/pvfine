package services

import (
	"fmt"
	"strconv"
	"strings"

	"pvfine/internal/pvf"
)

type shopLocation struct {
	path       []string
	occurrence int
	implicit   bool
}

type shopNode struct {
	element  pvf.ScriptElement
	values   []pvf.ScriptElement
	children []*shopNode
	location shopLocation
}

func parseShop(text string) *ShopDocument {
	doc := &ShopDocument{Categories: []ShopCategory{}, Tabs: []ShopTab{}, Issues: []PreviewIssue{}}
	root := &shopNode{}
	stack := []*shopNode{root}
	nodes := map[int]*shopNode{}
	ordered := []*shopNode{}
	counts := map[string]int{}
	for _, e := range pvf.ParseScriptView(text).Elements {
		if e.Kind == pvf.ScriptElementToken {
			if n := nodes[e.SectionID]; n != nil {
				n.values = append(n.values, e)
			}
			continue
		}
		if e.Kind != pvf.ScriptElementSection {
			continue
		}
		depth := len(e.SectionPath)
		if depth > len(stack) {
			continue
		}
		stack = stack[:depth]
		key := strings.Join(e.SectionPath, "/")
		n := &shopNode{element: e, location: shopLocation{path: e.SectionPath, occurrence: counts[key]}}
		counts[key]++
		parent := stack[len(stack)-1]
		parent.children = append(parent.children, n)
		nodes[e.SectionID] = n
		ordered = append(ordered, n)
		stack = append(stack, n)
	}
	categoryTypes := 0
	for _, n := range ordered {
		if strings.Join(n.element.SectionPath, "/") == "sell info/use category" {
			categoryTypes++
			doc.CategoryType = shopNodeFirst(n)
		}
	}
	if categoryTypes > 1 {
		shopIssue(doc, "use category", "商店包含重复的大分类声明")
	}
	consumed := map[*shopNode]bool{}
	addTab := func(n *shopNode, implicit bool) {
		name := shopNodeFirst(n)
		if implicit {
			name = "商品"
		}
		if name == "" {
			name = fmt.Sprintf("分页 %d", len(doc.Tabs)+1)
		}
		tab := ShopTab{Name: name, SourceStart: n.element.Start, Groups: []ShopGroup{}, location: n.location}
		tab.location.implicit = implicit
		tab.Implicit = implicit
		for _, child := range n.children {
			switch child.element.Section {
			case "item list", "sell item list":
				group := ShopGroup{SourceStart: child.element.Start, Items: []ShopEntry{}}
				parseShopList(child, &group, doc)
				consumed[child] = true
				tab.Groups = append(tab.Groups, group)
			case "category entry":
				group := ShopGroup{SourceStart: child.element.Start, Items: []ShopEntry{}}
				for _, field := range child.children {
					switch field.element.Section {
					case "id":
						group.CategoryID = shopNodeFirst(field)
					case "item list", "sell item list":
						parseShopList(field, &group, doc)
						consumed[field] = true
					}
				}
				if _, err := strconv.ParseInt(group.CategoryID, 10, 32); err != nil {
					shopIssue(doc, "category entry", "分类 ID 无效："+group.CategoryID)
				}
				tab.Groups = append(tab.Groups, group)
			}
		}
		doc.Tabs = append(doc.Tabs, tab)
	}
	for _, n := range root.children {
		switch n.element.Section {
		case "tab":
			addTab(n, false)
		case "sell info":
			direct := false
			for _, child := range n.children {
				switch child.element.Section {
				case "tab":
					addTab(child, false)
				case "item list", "sell item list", "category entry":
					direct = true
				}
			}
			if direct {
				addTab(n, true)
			}
		}
	}
	for _, n := range ordered {
		if (n.element.Section == "item list" || n.element.Section == "sell item list") && !consumed[n] {
			shopIssue(doc, n.element.Section, "无法识别商品列表所在的商店结构")
		}
	}
	categories := map[string]bool{}
	for _, tab := range doc.Tabs {
		for _, group := range tab.Groups {
			if group.CategoryID != "" && !categories[group.CategoryID] {
				categories[group.CategoryID] = true
				doc.Categories = append(doc.Categories, ShopCategory{ID: group.CategoryID})
			}
			if doc.CategoryType != "" && group.CategoryID == "" {
				shopIssue(doc, "category entry", "分类商品缺少分类 ID")
			}
		}
	}
	if doc.CategoryType == "" && len(doc.Categories) > 0 {
		shopIssue(doc, "use category", "存在分类商品，但未声明大分类类型")
	}
	if len(doc.Tabs) == 0 {
		shopIssue(doc, "sell info", "未找到可展示的商店分页，请检查文本结构")
	}
	return doc
}

func shopNodeFirst(n *shopNode) string {
	if len(n.values) > 0 {
		return n.values[0].Value
	}
	return ""
}

func parseShopList(n *shopNode, group *ShopGroup, doc *ShopDocument) {
	appendID := func(v pvf.ScriptElement) *ShopEntry {
		id, err := strconv.ParseInt(v.Value, 10, 32)
		if err != nil || id < 0 || v.TokenType != 0 {
			shopIssue(doc, n.element.Section, "商品 ID 无效："+v.Value)
			return nil
		}
		group.Items = append(group.Items, ShopEntry{Item: ShopItem{ID: v.Value}, SourceStart: v.Start, SourceEnd: v.End})
		return &group.Items[len(group.Items)-1]
	}
	if n.element.Section == "item list" {
		for _, v := range n.values {
			appendID(v)
		}
		return
	}
	if len(n.values) > 0 {
		shopIssue(doc, "sell item list", "商品列表包含无法识别的直接值")
	}
	for _, record := range n.children {
		if record.element.Section != "item" {
			shopIssue(doc, "sell item list", "商品列表包含无法识别的区块")
			continue
		}
		var index *shopNode
		for _, field := range record.children {
			if field.element.Section == "index" {
				if index != nil {
					shopIssue(doc, "index", "商品包含重复的 index 区块")
				}
				index = field
			}
		}
		if index == nil || len(index.values) != 1 {
			shopIssue(doc, "index", "商品缺少唯一的物品 ID")
			continue
		}
		// An index of -1 is an intentionally empty shop slot.
		if index.values[0].TokenType == 0 && index.values[0].Value == "-1" {
			continue
		}
		entry := appendID(index.values[0])
		if entry == nil {
			continue
		}
		for _, field := range record.children {
			name := field.element.Section
			if name != "price" && name != "need material" {
				continue
			}
			if name == "price" {
				entry.localGold = true
			} else {
				entry.localMaterials = true
			}
			entry.HasLocalCosts = true
			entry.localCosts = append(entry.localCosts, shopCostValues(name, field.values, entry.Item.ID, doc)...)
		}
	}
}
