package services

import (
	"fmt"
	"path"
	"strconv"
	"strings"

	"pvfine/internal/pvf"
)

// FileGUIService reads GUI documents and applies validated edits to the overlay.
type FileGUIService struct{ c *core }

func NewFileGUIService(c *core) *FileGUIService { return &FileGUIService{c: c} }

type ShopCategory struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type ShopCost struct {
	Kind     string          `json:"kind"`
	ItemID   string          `json:"itemId"`
	Quantity string          `json:"quantity"`
	Name     string          `json:"name"`
	Icon     *ImageReference `json:"icon"`
}
type ShopItem struct {
	FileIndex int32           `json:"fileIndex"`
	Path      string          `json:"path"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Icon      *ImageReference `json:"icon"`
	Costs     []ShopCost      `json:"costs"`
}
type ShopEntry struct {
	Item        ShopItem `json:"item"`
	SourceStart int      `json:"sourceStart"`
	SourceEnd   int      `json:"sourceEnd"`
}
type ShopGroup struct {
	CategoryID  string      `json:"categoryId"`
	SourceStart int         `json:"sourceStart"`
	Items       []ShopEntry `json:"items"`
}
type ShopTab struct {
	Name        string      `json:"name"`
	SourceStart int         `json:"sourceStart"`
	Groups      []ShopGroup `json:"groups"`
}
type ShopDocument struct {
	Revision     uint64         `json:"revision"`
	Name         string         `json:"name"`
	CategoryType string         `json:"categoryType"`
	Categories   []ShopCategory `json:"categories"`
	Tabs         []ShopTab      `json:"tabs"`
	Issues       []PreviewIssue `json:"issues"`
}

// ReadShop reads structure from the caller's draft, and references from the
// current archive overlay. Archive reads can populate caches, requiring Lock.
func (s *FileGUIService) ReadShop(fileIndex int32, text string) (*ShopDocument, error) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	if s.c.archive == nil {
		return nil, ErrNoArchive
	}
	if err := validateAnnotationIndex(s.c.archive, fileIndex); err != nil {
		return nil, err
	}
	if !strings.EqualFold(path.Ext(s.c.archive.Path(fileIndex)), ".shp") {
		return nil, fmt.Errorf("商店 GUI 仅支持 .shp 文件")
	}
	if s.c.annotationEngine == nil {
		return nil, fmt.Errorf("物品关联索引未初始化")
	}
	doc := parseShop(text)
	doc.Revision = s.c.batchRevision
	doc.Name = path.Base(s.c.archive.Path(fileIndex))
	if npc := firstSectionValue(text, "npc"); npc != "" {
		if ref, ok := s.c.resolveAnnotationReferenceLocked("npc", npc); ok && ref.Name != "" {
			doc.Name = resolvePreviewText(s.c.archive, ref.Name)
		}
	}
	jobs := map[string]string{"1": "附魔师", "0": "炼金术师", "3": "分解师", "2": "控偶师"}
	for i := range doc.Categories {
		category := &doc.Categories[i]
		switch doc.CategoryType {
		case "basic job":
			if ref, ok := s.c.resolveAnnotationReferenceLocked("职业", category.ID); ok {
				category.Name = ref.Name
			}
		case "expert job", "expert job non filter":
			category.Name = jobs[category.ID]
		}
		if category.Name == "" {
			category.Name = "分类 " + category.ID
		}
	}
	// Metadata and complete items are separate caches: materials need no costs,
	// and recursive material requirements must never recurse through products.
	metadata := make(map[string]ShopItem)
	resolve := func(id string) (ShopItem, int32, bool) {
		ref, ok := s.c.resolveAnnotationReferenceLocked("物品", id)
		if !ok {
			return ShopItem{ID: id, Name: "未知物品 #" + id, Costs: []ShopCost{}}, -1, false
		}
		if item, exists := metadata[id]; exists {
			return item, ref.FileIndex, true
		}
		item := ShopItem{ID: id, Name: ref.Name, FileIndex: ref.FileIndex, Path: s.c.archive.Path(ref.FileIndex), Costs: []ShopCost{}}
		if item.Name == "" {
			item.Name = "物品 #" + id
		}
		m, err := s.c.archive.ScriptMetadata(ref.FileIndex)
		if err == nil {
			item.Icon = imageReferenceFromPVF(m.Icon)
			if m.HasName {
				item.Name = markedName(m)
			}
		}
		metadata[id] = item
		return item, ref.FileIndex, true
	}
	items := make(map[string]ShopItem)
	for ti := range doc.Tabs {
		tab := &doc.Tabs[ti]
		tab.Name = resolvePreviewText(s.c.archive, tab.Name)
		for gi := range tab.Groups {
			for ei := range tab.Groups[gi].Items {
				entry := &tab.Groups[gi].Items[ei]
				id := entry.Item.ID
				if cached, ok := items[id]; ok {
					entry.Item = cached
					continue
				}
				item, index, ok := resolve(id)
				if !ok {
					shopIssue(doc, "item list", "找不到商品 #"+id)
				} else {
					itemText, err := s.c.archive.Text(index)
					if err != nil {
						shopIssue(doc, "item list", "无法读取商品 #"+id)
					} else {
						item.Costs = shopCosts(itemText, id, doc)
						for ci := range item.Costs {
							cost := &item.Costs[ci]
							if cost.Kind != "material" {
								continue
							}
							material, _, found := resolve(cost.ItemID)
							cost.Name, cost.Icon = material.Name, material.Icon
							if !found {
								shopIssue(doc, "need material", "找不到兑换道具 #"+cost.ItemID)
							}
						}
					}
				}
				items[id], entry.Item = item, item
			}
		}
	}
	return doc, nil
}

func shopIssue(doc *ShopDocument, section, message string) {
	doc.Issues = append(doc.Issues, PreviewIssue{Severity: "warning", Section: section, Message: message})
}

// parseShop uses semantic section paths and IDs rather than whitespace or
// line-based records, so repeated blocks retain their order and source range.
func parseShop(text string) *ShopDocument {
	doc := &ShopDocument{Categories: []ShopCategory{}, Tabs: []ShopTab{}, Issues: []PreviewIssue{}}
	view := pvf.ParseScriptView(text)
	values := make(map[int][]pvf.ScriptElement)
	for _, e := range view.Elements {
		if e.Kind == pvf.ScriptElementToken {
			values[e.SectionID] = append(values[e.SectionID], e)
		}
	}
	ti, gi := -1, -1
	categories := make(map[string]bool)
	for _, e := range view.Elements {
		if e.Kind != pvf.ScriptElementSection {
			continue
		}
		p := strings.Join(e.SectionPath, "/")
		vs := values[e.SectionID]
		first := ""
		if len(vs) > 0 {
			first = vs[0].Value
		}
		switch p {
		case "sell info/use category":
			doc.CategoryType = first
			if first != "basic job" && first != "expert job" && first != "expert job non filter" {
				shopIssue(doc, e.Section, "未知大分类类型："+first)
			}
		case "sell info/tab":
			if first == "" {
				first = fmt.Sprintf("分页 %d", len(doc.Tabs)+1)
			}
			doc.Tabs = append(doc.Tabs, ShopTab{Name: first, SourceStart: e.Start, Groups: []ShopGroup{}})
			ti, gi = len(doc.Tabs)-1, -1
		case "sell info/tab/category entry":
			if ti < 0 {
				continue
			}
			doc.Tabs[ti].Groups = append(doc.Tabs[ti].Groups, ShopGroup{SourceStart: e.Start, Items: []ShopEntry{}})
			gi = len(doc.Tabs[ti].Groups) - 1
		case "sell info/tab/category entry/id":
			if ti < 0 || gi < 0 {
				continue
			}
			doc.Tabs[ti].Groups[gi].CategoryID = first
			if _, err := strconv.ParseInt(first, 10, 32); err != nil {
				shopIssue(doc, e.Section, "分类 ID 无效："+first)
			}
		case "sell info/tab/item list", "sell info/tab/category entry/item list":
			if ti < 0 {
				continue
			}
			if p == "sell info/tab/item list" {
				doc.Tabs[ti].Groups = append(doc.Tabs[ti].Groups, ShopGroup{SourceStart: e.Start, Items: []ShopEntry{}})
				gi = len(doc.Tabs[ti].Groups) - 1
			}
			if gi < 0 {
				shopIssue(doc, e.Section, "商品列表缺少分类区块")
				continue
			}
			group := &doc.Tabs[ti].Groups[gi]
			for _, v := range vs {
				if _, err := strconv.ParseInt(v.Value, 10, 32); err != nil || v.TokenType != 0 {
					shopIssue(doc, e.Section, "商品 ID 无效："+v.Value)
					continue
				}
				group.Items = append(group.Items, ShopEntry{Item: ShopItem{ID: v.Value}, SourceStart: v.Start, SourceEnd: v.End})
			}
		default:
			if e.Section == "item list" {
				shopIssue(doc, e.Section, "无法识别商品列表所在的商店结构")
			}
		}
	}
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

func shopCosts(text, id string, doc *ShopDocument) []ShopCost {
	costs := []ShopCost{}
	view := pvf.ParseScriptView(text)
	sections := make(map[int][]pvf.ScriptElement)
	for _, e := range view.Elements {
		if e.Kind == pvf.ScriptElementToken {
			sections[e.SectionID] = append(sections[e.SectionID], e)
		}
	}
	validNumber := func(e pvf.ScriptElement) bool {
		n, err := strconv.ParseInt(e.Value, 10, 32)
		return err == nil && n >= 0 && e.TokenType == 0
	}
	for _, e := range view.Elements {
		if e.Kind != pvf.ScriptElementSection || len(e.SectionPath) != 1 {
			continue
		}
		vs := sections[e.SectionID]
		switch e.Section {
		case "price":
			if len(vs) != 1 || !validNumber(vs[0]) {
				shopIssue(doc, "price", "商品 #"+id+" 的金币价格无效")
				continue
			}
			costs = append(costs, ShopCost{Kind: "gold", Quantity: vs[0].Value, Name: "金币"})
		case "need material":
			if len(vs) == 0 || len(vs)%2 != 0 {
				shopIssue(doc, "need material", "商品 #"+id+" 的兑换道具必须按 ID、数量成对配置")
			}
			for i := 0; i+1 < len(vs); i += 2 {
				if !validNumber(vs[i]) || !validNumber(vs[i+1]) {
					shopIssue(doc, "need material", "商品 #"+id+" 的兑换成本无效")
					continue
				}
				costs = append(costs, ShopCost{Kind: "material", ItemID: vs[i].Value, Quantity: vs[i+1].Value})
			}
		}
	}
	return costs
}
