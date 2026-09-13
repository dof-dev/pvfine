# 90US 格式规格

## 总体布局

```
偏移          大小                    内容
0x00          0x30 (48B)             Header  —— "HeaD" 加密(可选 Guard)
0x30          FileCount × 24B        FileTable(明文)
...           HashTableSize          HashTable —— "HASH" 加密
...           NameTableSize          NameTable —— 内含 sTrA/sTrW 两个加密 zlib 段
...           GroupCount × 8B        GRPI(分块索引)—— "GRPI" 加密
...           BodySize               Body:GroupCount 个 zlib 分块,整块 "BodY" 加密
```

## 1. Header(解密后 48 字节,pack=1)

| 字段 | 类型 | 说明 |
|---|---|---|
| Signature | u32 | 明文恒为 `0x69706B6E`(LE 字节 `6E 6B 70 69`) |
| Guid | 20B | 本文件全 0 |
| FileCount | i32 | 本文件 1,008,171 |
| Padding | i32 | — |
| BodySize | i32 | 54,090,116 |
| GroupCount | i32 | 8,019 |
| HashTableSize | i32 | 9,868,348 |
| NameTableSize | i32 | 12,703,586 |

校验:`0x30 + FileCount*24 + HashTableSize + NameTableSize + GroupCount*8 + BodySize == 文件大小`。

## 2. 解密算法(LCG 流 XOR,即 MSVC `rand()` 参数)

四种 key:`"HeaD"`(头部)、`"HASH"`、`"GRPI"`、`"BodY"`(整块)用 magic `0x269EC3`;
`"sTrA"` / `"sTrW"`(Decrypt2)用 magic `0x269EC9`。

```python
def decrypt(data, key, magic=0x269EC3):
    k = key.encode()                       # 恰好 4 字节
    seed = (0x76826701*k[0] + 0x1C1*(k[3] + 0x1C1*(k[2] + 0x1C1*k[1]))) & 0xFFFFFFFF
    for each 4-byte block:
        t1   = (0x343FD*seed + magic) & 0xFFFFFFFF
        seed = (0x343FD*t1   + magic) & 0xFFFFFFFF
        xor  = (seed & 0xFFFF0000) | (t1 >> 16)      # 即 C# 的 (uint)((seed>>16)&0xFFFF + t1&0xFFFF0000)
        block ^= xor                                 # little-endian u32
    tail(0-3 字节): t1,t2 再各迭代一步,
        finalKey = (t1 & 0xFFFF0000) | (t2 >> 16),按字节 XOR
```

注意所有运算都是 C# `int` 溢出语义 = mod 2³²;`(seed>>16)&0xFFFF` 因子掩码,算术/逻辑移位等价。

**Guard 变体**:解密头部前先做 `header[24..28] ^= 0x55`。本文件使用 Guard=True。
两种都试,Signature 匹配者胜。

**种子变体**:另一批归档沿用同一 LCG(乘数 `0x343FD`、magic `0x269EC3`/`0x269EC9`),
但分段种子不再由 `"HeaD"/"HASH"/…` 这组固定字符串推出,标准密钥解不开头部。

这些种子是**该变体的固定常量,而非每个归档各不相同**:同版本先后两份归档
(其中一份经第三方工具修改过)的 header/GRPI/Body/sTrA/sTrW 五个种子完全相同,
因此直接内置于 `internal/pvf/variant.go` 的 `variantKeys()`:

| 段 | seed | magic |
|---|---|---|
| Header | `0x4A454634` | `0x269EC3` |
| GRPI | `0x1FBB7078` | `0x269EC3` |
| Body | `0xDD4FF706` | `0x269EC3` |
| sTrA | `0x712A98D4` | `0x269EC9` |
| sTrW | `0x712AE776` | `0x269EC9` |

若遇到种子未知的其它变体,`recoverHeader` 会从数据反解:签名明文固定为 `nkpi`,
可限定首个密钥流 dword 的低 16 位,余下 16 位再用「各段长度之和等于文件大小」
筛出唯一解;GRPI 用「最后一块累计压缩大小等于 `BodySize`」反解;Body 与
sTrA/sTrW 则利用 zlib 头部 `78 xx` 这两个已知明文,并用解压长度必须等于
GRPI/NameTable 声明值来确认。

**HASH 段**:该变体的 HASH 种子未能反解(其内容既非偏移量也非文件表顺序,
排序列表也不呈升序,多种已知明文攻击均不成立)。由于该段只参与段偏移计算、
代码从不读取其内容,保存时**原样保留原始密文**,不再用标准密钥重写,
以免破坏变体客户端的查找表。标准变体仍按原逻辑重建。


## 3. FileTable(明文,每条 24 字节,pack=1)

```c
struct PvfFileItem {
    int NameOffset;   // 字符串池魔数偏移(见 §5)
    int PathOffset;   // 同上,存目录部分
    int ChunkIndex;   // 第几个 Body 分块
    int DataOffset;   // 文件数据在解压后分块内的偏移
    int DataSize;
    int DataType;     // 1 = 脚本 token 流;3 = 本地化文本(UTF-16)
};
```

完整路径 = `PathOffset 解析串 + "/" + NameOffset 解析串`。

## 4. GRPI(解密后每条 8 字节)

```c
struct GrpiItem { int CompressedSize; int OriginalSize; };
```
`CompressedSize` 是**累计**值。第 i 块 Body 字节区间 =
`[bodyOffset + g[i-1].CompressedSize, bodyOffset + g[i].CompressedSize)`。
整块 `"BodY"` 解密 → 标准 zlib 解压(0x78 0x9C 头 + Adler32 尾)。
`OriginalSize` 为该块解压后大小。本文件 54.1MB → 583.6MB(10.79 倍)。

## 5. NameTable(字符串池)

布局:8 字节保留头 + 两个 section:

```
sTrA: u32 encSize ^ 0xAA74472E ; u32 rawLen ^ encSize ; encSize 字节密文
sTrW: u32 encSize ^ 0x9A82F037 ; u32 rawLen ^ encSize ; encSize 字节密文
```
密文:`Decrypt2(key)` → zlib 解压。sTrA = UTF-8 以 `\0` 分隔;sTrW = UTF-16LE 以 `\0\0` 分隔。
本文件:sTrA 63,000,914B;sTrW 7,035,304B。

**魔数偏移**(NameOffset/PathOffset/token 字符串值通用):
- 偶数 → sTrA,字节偏移 = `off >> 1`
- 奇数 → sTrW,字节偏移 = `(off >> 1) * 2`

## 6. DataType 1 脚本格式

Token 流,每个 token 5 字节:`u8 type + i32 value`。

| type | 含义 | 反编译表现 |
|---|---|---|
| 0 | 整数 | 直接输出数值 |
| 2 | 浮点(value 按 f32 位型解释) | 输出浮点 |
| 3 | 标签串(字符串池偏移) | 独占一行,如 `[name]` |
| 5 | 块串开头 | `{5=\`...\`}` |
| 6 | 行内串 | `` `...` `` |
| 7 | 块串(另一形式) | `{7=\`...\`}` |

## 7. DataType 3 本地化文本(.str 等)

UTF-16LE 明文。本文件存在韩服转制痕迹:内容实为 **EUC-KR 字节被逐字节按 CP437
字体映射进 UTF-16 码位**(如 `한국` = `C7 D1` → `╟╤`)。
还原:每个字符 `chr.encode('cp437')` 拼回字节流 → `cp949` 解码。
`pvflib.py` 已内置自动检测与还原(`_fix_korean_mojibake`)。

## 8. HashTable(供客户端按路径查找)

解密后:`u32 count` + `count × (u32 NameOffset, u32 PathOffset)` + `u32 n` + `n × u32`
(按解析串字典序排序的偏移表,二分用)。

## 解析结果摘要

- 文件 1,008,171 个,分块 8,019 个,全部解压成功,0 引用越界
- 扩展名分布:.ani 570,617 / .equ 148,594 / .act 77,955 / .stk 42,219 / .obj 21,974 / .ai 21,534 / .til 20,792 / .atk 18,896 / .key 16,322 / .map 15,921 …
- 顶层目录:equipment 408,432 / monster 211,668 / passiveobject 162,065 / map 79,046 / stackable 46,390 / character 40,203 …
