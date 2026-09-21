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

### 5.1 写入时该放哪个池(新文本必须遵守,否则游戏里乱码)

客户端**只从 sTrW 读非 ASCII 文本**,所以新输入的中文/韩文必须写进 sTrW(奇偏移)。
实测两代归档的既有文本都严格遵循这条:

| 归档 | ASCII → sTrA(偶) | 非 ASCII → sTrW(奇) | 反例 |
|---|---|---|---|
| `90CN\Script.pvf`(Protected 变体,105 万文件) | 9,904 条 | 1,141 条 | 0 |
| `testdata/110US.pvf`(Paged110) | 0(其 sTrA 为空) | 24,821 条(ASCII 也在 sTrW) | 0 |

因此写入规则(`StringOffset` / `scriptStringOffset`):

1. 先按原样查两个池的索引,命中就沿用原偏移;
2. 未命中时:**非 ASCII → sTrW**;ASCII → 若归档的 sTrA 非空则进 sTrA,否则也进 sTrW
   (110US 的 sTrA 为空,该客户端把所有字符串都放在 sTrW)。

> 旧实现把一切都 append 到 sTrA,于是新输入的中文被写成 UTF-8、用偶偏移引用,
> 客户端按自己的约定取不到这段文本 → 游戏里显示乱码。已修,并有回归测试
> (`TestStringOffsetPoolConvention`、`TestVariantChineseNameEncoding`)。
>
> 另外 90CN 的 sTrA 里本来就有 13.7 万个非 ASCII 字节(历史遗留),所以校验方式是
> 「新增文本没有让 sTrA 的非 ASCII 字节变多」,而不是「sTrA 纯 ASCII」。

## 6. DataType 1 脚本格式

Token 流,每个 token 5 字节:`u8 type + i32 value`。

| type | 含义 | 反编译表现 |
|---|---|---|
| 0 | 整数 | 直接输出数值 |
| 2 | 浮点(value 按 f32 位型解释) | 输出浮点;整值写成 `8000.0` 以区分于整数 |
| 3 | 标签串(字符串池偏移) | 独占一行,如 `[name]` |
| 5 | 块串开头 | `{5=\`...\`}` |
| 6 | 行内串 | `` `...` `` |
| 7 | 块串(另一形式) | `{7=\`...\`}` |
| 8 | 字符串池引用(Paged110) | `{8=\`...\`}`,如 `{8=\`<31::equip_name_1>\`}` |
| 10 | 字符串池引用(Paged110) | `{10=\`...\`}`,多行命令文本 |

## 7. DataType 3 本地化文本(.str 等)

UTF-16LE 明文。本文件存在韩服转制痕迹:内容实为 **EUC-KR 字节被逐字节按 CP437
字体映射进 UTF-16 码位**(如 `한국` = `C7 D1` → `╟╤`)。
还原:每个字符 `chr.encode('cp437')` 拼回字节流 → `cp949` 解码。
`pvflib.py` 已内置自动检测与还原(`_fix_korean_mojibake`)。

**写回**:编辑器读到的是还原后的可读韩文,保存时按**原载荷是否伪画**决定编码 ——
`SetText` 对 TypeUnicode 会在原载荷通过 `looksLikeCP437Painting` 时用
`EncodeKoreanMojibake` 重新伪画回写,否则写普通 UTF-16LE。这样「打开→改→保存」
不会把可读韩文变成乱码(检测基于整段载荷的前 2000 字符,短载荷可能判不出)。

## 8. HashTable(供客户端按路径查找)

解密后:`u32 count` + `count × (u32 NameOffset, u32 PathOffset)` + `u32 n` + `n × u32`
(按解析串字典序排序的偏移表,二分用)。长度满足
`hashSize = 4 + count*8 + 4 + n*4`。

密钥名:
- 标准 90US(LegacyNkpi):`keySeed("HASH")` + magic `0x269EC3`。
- Protected 变体:**`wideSeed("hash")`**(全小写)+ magic `0x269EC3`。
- Paged110:密钥名未取到,但**可由结构反解**(见下),110US 解出
  `seed=0x5824C072`、magic `0x269EC3`。

**反解**:段首 dword 就是条目数(等于文件数;90US 变体族会多 19 条,故在
文件数 ±64 的窗口内搜)。已知这个明文 dword 就钉住了首个 keystream dword 的低
16 位,每个 (magic, count) 只剩 ≤4 个候选种子;再用结构验证:长度精确满足
`4+count*8+4+n*4`、抽样的每个 NameOffset/PathOffset 都能解析出池内字符串、
尾部偏移表按串严格递增。实现见 `internal/pvf/hashtable.go` 的
`recoverHashSeed`;解析时若密钥集未给出 HASH 种子会自动尝试。

## 解析结果摘要

- 文件 1,008,171 个,分块 8,019 个,全部解压成功,0 引用越界
- 扩展名分布:.ani 570,617 / .equ 148,594 / .act 77,955 / .stk 42,219 / .obj 21,974 / .ai 21,534 / .til 20,792 / .atk 18,896 / .key 16,322 / .map 15,921 …
- 顶层目录:equipment 408,432 / monster 211,668 / passiveobject 162,065 / map 79,046 / stackable 46,390 / character 40,203 …

## 9. Paged110 变体（110US 等新客户端）

新客户端在 90US 逻辑布局之上加了一层「页首保护」:

- 每 **10 MiB 页的前 10,240 字节**用该页 **32 字节密钥做 AES-256-CBC（IV=0）**
  加密;页内其余字节仍是普通 90US 数据。Guards **就地覆盖逻辑流**
  （物理长度 == 逻辑长度),所以把页首块解密后,整份文件与 §3–§8 的布局一致。
- **页密钥表不在归档内**:同目录 `sk.dat`（1920 B）是 RSA-1024 PKCS#1 v1.5
  密封的密文,私钥内置于客户端 `DFO.exe`（实现里已内置该私钥）。逐 128 字节块
  解密后得到 **52 × 32 字节**页密钥表(本文件 52 页)。
- 该表还要再整体做一次 **AES-256-CBC（IV=0）** 解包,密钥是 `DFO.exe` 中一段
  **64 个十六进制字符**的「metadata key」（本客户端为 `21ad8ff2…4489`,
  实现里已内置,必要时会改为扫描可执行文件得到候选）。
  只解包前 `len & ~0xFF` 字节,尾部余数不动。
- 分段密钥名改为:**`iNfO`**(头)/ **`Gidx`**(GRPI)/ **`stAs`**/**`stWs`**(名称池)/
  **`mAIn`**(body),种子用宽字符公式(与 Protected 变体相同,见 §2 的种子变体),
  名称池长度掩码为 `3886938090` / `3101599660`。
- 头部**没有** `0x55` guard。
- **写回已支持**(`SaveTo`):打开时保留解包后的页密钥表(`Archive.pageKeys`),
  写出时按页对前 10,240 字节重新做 AES-256-CBC(IV=0) 加密(`writePageGuarded`
  流式写,不需要第二份整库内存)。未修改的归档重新写出**逐字节等于原文件**
  (已用 543 MB 的 `110US.pvf` 的 MD5 验证)。
- **增删文件也已支持**:HASH 段的种子可从结构反解(§8),所以重建时能重新生成
  HASH、重建名称池;Paged110 的池段用**自己的**密钥与长度掩码写回
  (`buildNameTable` 过去硬编码 90US 的 `sTrA/sTrW` 与掩码,导致重开时池整个读不到,
  已修)。代价是要重新压缩 401 MB 的名称池(实测约 35 秒/次);只改文件内容的编辑
  不触发池重建。
- 若 `pageKeys` 不在内存(理论上不会),返回 `ErrPaged110ReadOnly`;若 HASH 种子
  既没给出也解不出来,结构性编辑返回 `ErrPaged110StructureLocked`。
### 9.1 字符串表与 `<表号::键名>` 占位符

Paged110 的 `.equ`/`.stk` 不直接存显示文本,`[name]` 等字段是 type 8/10 token,
内容是 `<表号::键名>` 占位符(如 `<31::equip_name_1>`)。解析方式:

- 映射文件按**优先级递减**(靠前者胜):`list/n_string.lst` → `n_string.lst` →
  `list/n_string_translate.lst` → `n_string_translate.lst` →
  `list/n_string_kor.lst` → `n_string_kor.lst`。
  基准表 `list/n_string.lst` 指向 `String/*.uv.str`,存的是客户端自己显示的语言
  (110US 里是中文/英文);`*.kor.str` / `*.translate.str` 是语言覆盖层,只映射
  3、13 两个表号,仅用于补基准表的空缺。**基准表不能排在后面**:那样会在基准表
  有中文时也显示韩文。
  `.lst` 是普通 token 流:`整数`(表号) + `字符串`(`.str` 路径) 交替。
- `.str` 是 UTF-16LE 文本,每行 `键>值`,`//` 注释;路径缺失时回退到
  `.translate.str` / `.kor.str` / `.uv.str` 变体。
- 查表从基准表往下取**第一个非空**值:基准表用 `键=` 列出所有已知键,其中装备名
  有 36.7% 是空值(未翻译),空值不能当命中。`ResolveStringTable` 同时回报来源与
  是否回退。
- 解析不到时**原样保留**占位符。解析是只读的:归档里存的就是占位符,
  脚本文本不会被改写。
- 装备悬浮预览(`services/equipment_preview.go`)、文件树标签、搜索结果、编辑器标注
  四处一致:只由覆盖层答出的名字会追加 `（未翻译）` 标记
  (`ScriptMetadata.NameFallback` + `services.markedName`),避免把韩文原文误当成
  该客户端的文案。
- 编辑器不下手改文档,而是把译文作为一条 `type = "placeholder"` 的标注标签挂在
  占位符后面(tooltip 给出原始占位符与来源 `.str`;来源 <8 MB 时 Cmd/Ctrl+单击
  可打开该字符串表)。搜索/树的名称(`ScriptMetadata`)同样取解析后的文本,
  因此中文名可搜。
- **就地修改译文**:单击标签 → 弹框 → `SetPlaceholderText`。目标表取「真正答出该键
  的那一张」(可能是覆盖层);没有任何表有该键时**追加到基准表**(插在尾部 NUL 填充
  之前 —— 读取端在第一个 NUL 处截断,写在填充之后就读不到)。
  改动按 **UTF-16LE 字节就地替换**那一行的值,BOM/行尾符/其余字节不变;
  解析不了的表拒绝写入。改完立刻刷新编辑器标注、文件树标签与搜索索引里该文件的
  名称,并写入版本历史。改动先落在内存 overlay,需保存才写回归档。
- **新建文件的名称**:引用了表里还没有的键时,标签显示红色「未定义」,点它即可创建
  条目;工具栏「字符串引用」会插入 `{8=`<表号::键名>`}` 并写入对应表。
  注意标记里的 8 是 token 类型,表号在 `<表号::键名>` 中;写完还要把文件注册进
  `list/equipment.lst`(或 `list/stackable.lst`)才可被客户端按 ID 找到。

### 9.2 Paged110 的列表布局(`list/*.lst`)

90US 把列表放在被索引目录旁边、条目按列表目录相对存放;110US 把所有列表集中到
`list/` 下、条目改为**归档根相对**:

| | 90US | 110US |
|---|---|---|
| 装备列表 | `equipment/equipment.lst` | `list/equipment.lst` |
| 条目 | `character/a.equ` | `equipment/character/a.equ` |

`Archive.FindList` 两种布局都试(配置 90US 路径也能在新归档上解析),
`listPathCandidates` 对条目先按列表目录解释、再按归档根解释(各自再试 `(r)` 变体)。
`ScriptMetadata` 支持 8/10 token 并对名称做占位符解析,所以搜索索引、文件树标签
用的是显示文本而不是 `<表号::键名>`(只由覆盖层答出的还会加 `（未翻译）`)。

### 9.3 `list/*_indexhash.etc`(列表的伴随索引)

110US 的每个列表还配一个 `<列表名>_indexhash.etc`(90US 没有)。格式与 `.lst` 相同:
`(type 0 整数 id)(type 6 字符串)`,每条 10 字节;第二个 token 的字符串内容是**十进制
uint32**(超过 2^31 所以用字符串存)。实测该值**只与 id 有关**(不同列表里同一 id 的
值相同,即使路径完全不同)。生成函数为两轮 `uint32` 混合：
`x = (x ^ (x >> 16)) * 0x45d9f3b`，最后再执行一次 `x ^ (x >> 16)`。
归档中少量历史/特殊条目可能保留其它值；新增普通列表条目使用该函数。
索引文件是列表的**严格超集**(原厂工具维护列表时会顺带补齐),因此新增 id 会缺条目。

110US 还有一个容易踩的兼容点：第二 token 虽然是 ASCII 十进制文本，仍必须引用
UTF-16 字符串池 `sTrW`（magic offset 为奇数）。写到 UTF-8 池 `sTrA` 后，解析器
仍可能读回正确数字，但客户端读取新增条目时会出错。

工具:`Archive.IndexHashCompanionPath` / `IndexHashPairs` / `IndexHashGaps` /
`IndexHashSiblingPaths` / `IndexHashValue` / `SetIndexHashEntryForID` /
`SetIndexHashEntry`(值写成十进制文本,其余条目字节不变)。
详见 `docs/PVF新格式分析.md` §2.16。

