import { describe, expect, it } from 'vitest';
import { effectFrameIndex, particleInstances, type ParticleMotion } from '../src/gui/effectPlayback';
describe('特效预览时序与粒子运动', () => {
 it('按原始时序循环或停在尾帧，处理空动画及零时长', () => {
  expect(effectFrameIndex([],0,true)).toBe(-1);
  expect(effectFrameIndex([100,200],150,true)).toBe(1);
  expect(effectFrameIndex([100,200],350,true)).toBe(0);
  expect(effectFrameIndex([100,200],350,false)).toBe(1);
  expect(effectFrameIndex([0,0],20,true)).toBe(1);
 });
 const meta: ParticleMotion={life:1000,frequency:100,maximum:1000,maximumTime:0,gravity:100,position:[10,0,0],area:[0,0,0],alpha:[100,0],velocity:[20,0,0],acceleration:[0,0,0]};
 it('限制实例数量，按寿命、速度、重力和透明度计算', () => {
  const instances=particleInstances(meta,500);
  expect(instances).toHaveLength(6); expect(instances[0]).toEqual({x:20,y:12.5,alpha:.5,age:500});
  expect(particleInstances({...meta,life:60000,frequency:16},2000).length).toBeLessThanOrEqual(64);
 });
 it('生成时间截止后不创建新粒子，重播保持确定性', () => {
  const value={...meta,maximumTime:100};
  expect(particleInstances(value,500)).toHaveLength(2);
  expect(particleInstances(meta,500)).toEqual(particleInstances(meta,500));
  expect(particleInstances({...meta,frequency:-1},200)).toHaveLength(1);
 });
});
