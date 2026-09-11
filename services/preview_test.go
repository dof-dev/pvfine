package services

import "testing"

func TestPreviewServiceParseANI(t *testing.T) {
	result, err := NewPreviewService().ParseANI("[FRAME MAX]\n1\n[FRAME000]\n[IMAGE]\n`Item/test.img` 3\n[IMAGE POS]\n2 -4\n[DELAY]\n25")
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || !result.Valid || result.FrameMax != 1 || len(result.Frames) != 1 {
		t.Fatalf("preview result = %#v", result)
	}
	frame := result.Frames[0]
	if frame.Delay != 25 || len(frame.Layers) != 1 {
		t.Fatalf("preview frame = %#v", frame)
	}
	layer := frame.Layers[0]
	if layer.Image.Path != "Item/test.img" || layer.Image.Index != 3 || layer.X != 2 || layer.Y != -4 {
		t.Fatalf("preview layer = %#v", layer)
	}
}
