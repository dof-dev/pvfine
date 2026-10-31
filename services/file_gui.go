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
	Rarity    int32           `json:"rarity"`
	Icon      *ImageReference `json:"icon"`
	Costs     []ShopCost      `json:"costs"`
}
type ShopEntry struct {
	Item                      ShopItem `json:"item"`
	SourceStart               int      `json:"sourceStart"`
	SourceEnd                 int      `json:"sourceEnd"`
	HasLocalCosts             bool     `json:"hasLocalCosts"`
	localCosts                []ShopCost
	localGold, localMaterials bool
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
	Implicit    bool        `json:"implicit"`
	location    shopLocation
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
	if doc.CategoryType != "" {
		categories, err := readShopCategories(s.c.archive, doc.CategoryType)
		if err != nil {
			shopIssue(doc, "use category", err.Error())
		} else {
			doc.Categories = categories
		}
		unclassified := false
		for _, tab := range doc.Tabs {
			for _, group := range tab.Groups {
				if group.CategoryID == "" && len(group.Items) > 0 {
					unclassified = true
				}
			}
		}
		if unclassified {
			doc.Categories = append([]ShopCategory{{ID: "", Name: "未分类"}}, doc.Categories...)
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
		item := ShopItem{ID: id, Name: ref.Name, FileIndex: ref.FileIndex, Path: s.c.archive.Path(ref.FileIndex), Rarity: pvf.RarityUnknown, Costs: []ShopCost{}}
		if item.Name == "" {
			item.Name = "物品 #" + id
		}
		m, err := s.c.archive.ScriptMetadata(ref.FileIndex)
		if err == nil {
			item.Icon = imageReferenceFromPVF(m.Icon)
			item.Rarity = m.RarityValue()
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
	for ti := range doc.Tabs {
		for gi := range doc.Tabs[ti].Groups {
			for ei := range doc.Tabs[ti].Groups[gi].Items {
				entry := &doc.Tabs[ti].Groups[gi].Items[ei]
				if !entry.localGold && !entry.localMaterials {
					continue
				}
				costs := []ShopCost{}
				for _, cost := range entry.Item.Costs {
					if cost.Kind == "gold" && entry.localGold || cost.Kind == "material" && entry.localMaterials {
						continue
					}
					costs = append(costs, cost)
				}
				for _, cost := range entry.localCosts {
					if cost.Kind == "material" {
						material, _, found := resolve(cost.ItemID)
						cost.Name, cost.Icon = material.Name, material.Icon
						if !found {
							shopIssue(doc, "need material", "找不到兑换道具 #"+cost.ItemID)
						}
					}
					costs = append(costs, cost)
				}
				entry.Item.Costs = costs
			}
		}
	}
	return doc, nil
}

func shopIssue(doc *ShopDocument, section, message string) {
	doc.Issues = append(doc.Issues, PreviewIssue{Severity: "warning", Section: section, Message: message})
}

const shopCategoryPath = "etc/itemshop/itemshopcategory.etc"

// readShopCategories keeps the order and IDs defined by the active archive.
func readShopCategories(a *pvf.Archive, categoryType string) ([]ShopCategory, error) {
	index, ok := a.Find(shopCategoryPath)
	if !ok {
		return nil, fmt.Errorf("找不到商店分类文件：%s", shopCategoryPath)
	}
	text, err := a.Text(index)
	if err != nil {
		return nil, fmt.Errorf("读取商店分类文件失败：%w", err)
	}
	view := pvf.ParseScriptView(text)
	values := make(map[int]string)
	for _, e := range view.Elements {
		if e.Kind == pvf.ScriptElementToken && e.Index == 0 {
			values[e.SectionID] = e.Value
		}
	}
	type categoryBlock struct {
		name    string
		entries []ShopCategory
	}
	blocks := []categoryBlock{}
	block, entry := -1, -1
	for _, e := range view.Elements {
		if e.Kind != pvf.ScriptElementSection {
			continue
		}
		switch strings.Join(e.SectionPath, "/") {
		case "itemshop category":
			blocks = append(blocks, categoryBlock{entries: []ShopCategory{}})
			block, entry = len(blocks)-1, -1
		case "itemshop category/category name":
			if block >= 0 {
				blocks[block].name = values[e.SectionID]
			}
		case "itemshop category/category entry":
			if block >= 0 {
				blocks[block].entries = append(blocks[block].entries, ShopCategory{})
				entry = len(blocks[block].entries) - 1
			}
		case "itemshop category/category entry/id":
			if block >= 0 && entry >= 0 {
				blocks[block].entries[entry].ID = values[e.SectionID]
			}
		case "itemshop category/category entry/text":
			if block >= 0 && entry >= 0 {
				blocks[block].entries[entry].Name = resolvePreviewText(a, values[e.SectionID])
			}
		}
	}
	for _, block := range blocks {
		if block.name != categoryType {
			continue
		}
		categories := make([]ShopCategory, 0, len(block.entries))
		for _, category := range block.entries {
			if _, err := strconv.ParseInt(category.ID, 10, 32); err != nil {
				continue
			}
			if category.Name == "" {
				category.Name = "分类 " + category.ID
			}
			categories = append(categories, category)
		}
		return categories, nil
	}
	return nil, fmt.Errorf("商店分类文件中找不到大分类：%s", categoryType)
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
	for _, e := range view.Elements {
		if e.Kind != pvf.ScriptElementSection || len(e.SectionPath) != 1 {
			continue
		}
		costs = append(costs, shopCostValues(e.Section, sections[e.SectionID], id, doc)...)
	}
	return costs
}

func shopCostValues(name string, vs []pvf.ScriptElement, id string, doc *ShopDocument) []ShopCost {
	costs := []ShopCost{}
	validNumber := func(e pvf.ScriptElement) bool {
		n, err := strconv.ParseInt(e.Value, 10, 32)
		return err == nil && n >= 0 && e.TokenType == 0
	}
	switch name {
	case "price":
		if len(vs) != 1 || !validNumber(vs[0]) {
			shopIssue(doc, "price", "商品 #"+id+" 的金币价格无效")
			return costs
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
	return costs
}
