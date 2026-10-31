package services

import "strings"

// ReadItemNames resolves equipment and stackable names using the same item
// relations and localized text as other archive views. Missing IDs are omitted.
func (s *FileGUIService) ReadItemNames(ids []string) (map[string]string, error) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	if s.c.archive == nil {
		return nil, ErrNoArchive
	}
	result := make(map[string]string)
	if s.c.annotationEngine == nil {
		return result, nil
	}
	seen := make(map[string]bool)
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		if reference, ok := s.c.resolveAnnotationReferenceLocked("物品", id); ok {
			result[id] = reference.Name
		}
	}
	return result, nil
}

// ReadItemIcons reads only display metadata for the currently visible rewards.
func (s *FileGUIService) ReadItemIcons(ids []string) (map[string]*ImageReference, error) {
	appearances, err := s.ReadItemAppearances(ids)
	if err != nil {
		return nil, err
	}
	result := make(map[string]*ImageReference, len(appearances))
	for id, appearance := range appearances {
		result[id] = appearance.Icon
	}
	return result, nil
}

// ItemAppearance shares the item rarity and icon metadata used by shop views.
type ItemAppearance struct {
	Icon   *ImageReference `json:"icon"`
	Rarity int32           `json:"rarity"`
}

// ReadItemAppearances batches presentation metadata without reading item prices.
func (s *FileGUIService) ReadItemAppearances(ids []string) (map[string]*ItemAppearance, error) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	if s.c.archive == nil {
		return nil, ErrNoArchive
	}
	result := make(map[string]*ItemAppearance)
	if s.c.annotationEngine == nil {
		return result, nil
	}
	seen := make(map[string]bool)
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		if reference, ok := s.c.resolveAnnotationReferenceLocked("物品", id); ok {
			if metadata, err := s.c.archive.ScriptMetadata(reference.FileIndex); err == nil {
				result[id] = &ItemAppearance{Icon: imageReferenceFromPVF(metadata.Icon), Rarity: metadata.RarityValue()}
			}
		}
	}
	return result, nil
}
