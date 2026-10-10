import { RangeSet, RangeValue } from "@codemirror/state";
import type { EditorAnnotation } from "../bindings/pvfine/services/models";

/** 位置信息由 RangeSet 保存；编辑时共享未受影响的树节点和标注对象。 */
export class AnnotationRange extends RangeValue {
  readonly annotation: EditorAnnotation;
  constructor(annotation: EditorAnnotation) {
    super();
    this.annotation = annotation;
  }
  startSide = 1;
  endSide = -1;
}

export function indexAnnotations(annotations: readonly EditorAnnotation[], length: number): RangeSet<AnnotationRange> {
  return RangeSet.of(annotations.map((annotation) => {
    const from = Math.max(0, Math.min(length, annotation.start));
    const to = Math.max(from, Math.min(length, annotation.end));
    return new AnnotationRange(annotation).range(from, to);
  }), true);
}

/** 仅遍历可见文本内的标注；折叠造成多个可见范围时同一标注只返回一次。 */
export function forEachVisibleAnnotation(
  ranges: RangeSet<AnnotationRange>,
  visibleRanges: readonly { from: number; to: number }[],
  callback: (from: number, to: number, value: AnnotationRange) => void,
): void {
  const seen = new Set<AnnotationRange>();
  for (const visible of visibleRanges) {
    ranges.between(visible.from, visible.to, (from, to, value) => {
      if (seen.has(value)) return;
      seen.add(value);
      callback(from, to, value);
    });
  }
}

export function referenceAt(ranges: RangeSet<AnnotationRange>, position: number): EditorAnnotation | undefined {
  let result: EditorAnnotation | undefined;
  ranges.between(position, position, (from, to, value) => {
    if (from <= position && position < to && value.annotation.targetFileIndex >= 0) {
      result = value.annotation;
      return false;
    }
  });
  return result;
}

export function annotationAt(ranges: RangeSet<AnnotationRange>, position: number): EditorAnnotation | undefined {
  let result: EditorAnnotation | undefined;
  ranges.between(position, position, (from, to, value) => {
    if (from <= position && position < to) {
      result = value.annotation;
      return false;
    }
  });
  return result;
}
