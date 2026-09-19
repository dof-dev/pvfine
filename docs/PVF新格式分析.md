# 110US.pvf 新格式分析笔记

本文记录对 `testdata/110US.pvf`（543,423,313 字节）的实测结论，以及从
`testdata/DFO.exe`（212,122,032 字节，Themida 保护）+ `testdata/sk.dat`
（1,920 字节）反推出来的新版本密钥推导。结论分「已确证」与「待解决」两部分。

## 1. 110US.pvf 的实测结构

### 1.1 文件表是明文，且能直接读

24 字节一条，字段与 90US 完全一致：

```
int NameOffset, PathOffset, ChunkIndex, DataOffset, DataSize, DataType;
```

| 项 | 实测值 |
|---|---|
| 表起始（物理） | 0x2808 |
| 表结束 | 0x62AD830（≈98.6 MiB）|
| 记录数 | ≈ 4,310,872 |
| DataType | 1（脚本）/ 3（文本）|
| 同 chunk 内 DataOffset 连续、ChunkIndex 单调递增 | 是 |

注意 `(0x2808 - 0x30) % 24 == 0`，即记录栅格与「标准布局：0x30 起表」一致。

### 1.2 每 10 MiB 有一个 ~10248 字节的加密块

块 0 位于 `0..0x2808`；此后在 `0xA00000`、`0x1400000`、… 每个 10 MiB
边界上都有（整文件约 52 个）。这些块把记录流切开，但长度是 24 的整数倍，
因此记录栅格不变。

实测：块内容高熵、无自相关、非 zlib、用任何已知 LCG 种子在块内任意 48 字节
窗口都解不出 `nkpi`。

### 1.3 表格之后

`0x62AD830` 起为高熵区（全文件扫描无任何明文 zlib 流），一直延续到文件尾
（约 440 MB）：即 hash / 名称池 / GRPI / body，处于「压缩 + 加密」状态。

## 2. DFO.exe 中的新密钥推导（已确证）

### 2.1 新种子公式

`DFO.exe` RVA `0x6A997E0`（magicMain）与 `0x6ADDAB0`（magicAlt）是两段
逐段加解密例程，开头即为种子推导（反汇编节选，字面量已核对）：

```
41 0f b7 40 02      movzx eax, word ptr [r8+2]      ; k1（16 位单元）
44 69 c8 93 03 00 00 imul r9d, eax, 0x393
41 0f b7 40 04      movzx eax, word ptr [r8+4]      ; k2
44 03 c8            add   r9d, eax
41 0f b7 40 06      movzx eax, word ptr [r8+6]      ; k3
45 69 d1 93 03 00 00 imul r10d, r9d, 0x393
44 03 d0            add   r10d, eax
41 0f b7 00         movzx eax, word ptr [r8]        ; k0
44 69 c0 11 97 9e 33 imul r8d, eax, 0x339E9711
45 69 ca 93 03 00 00 imul r9d, r10d, 0x393
45 03 c8            add   r9d, r8d
```

即：

```go
// 密钥为 4 个 16 位单元（宽字符串，即 UTF-16 字符）
seed = 0x339E9711*u0 + 0x393*(u3 + 0x393*(u2 + 0x393*u1))
```

与 90US 的旧公式 `0x76826701*k[0] + 0x1C1*(...)` 结构相同但常量不同，
且键改为 16 位单元（`movzx word`），所以旧实现必然解不开。

### 2.2 密钥名已反解（穷举 4 字符名匹配已知种子）

用上面的公式穷举 4 字符密钥名，恰好命中 `internal/pvf/variant.go` 里
早已内置的「变体固定常量」：

| 段 | 密钥名 | 新公式算出的 seed | 本仓库 variantKeys() |
|---|---|---|---|
| Header | `hEAd` | `0x4A454634` | `0x4A454634` |
| GRPI | `grpi` | `0x1FBB7078` | `0x1FBB7078` |
| Body | `bODy` | `0xDD4FF706` | `0xDD4FF706` |
| sTrA | `StRa` | `0x712A98D4` | `0x712A98D4` |
| sTrW | `StRw` | `0x712AE776` | `0x712AE776` |

结论：所谓「变体」其实**就是新版本（宽字符串密钥）**，此前只能靠反解得到
常量，现在可以直接用公式算；`HASH` 段的名字仍未知（当时未能反解出该段种子），
但现在可以用「名字 → seed → 解 HASH 段并校验」的方式穷举 4 字符名补上。

已验证：`Script.pvf` 与 `Script3.pvf` 的头部都是 `hEAd`（magicMain，
guard=false）解开的——它们属于这一族。

### 2.3 头部读取路径（0x6A99960..0x6A9B230）

该函数：`fopen` → 读入内存 → 构造流对象 → **分页处理** → 拷贝并解密 48 字节
头部 → `cmp dword [buf], 'nkpi'` → 读 `FileCount*24` 的文件表。

分页循环（关键指令）：

```
0x6A9A158  lea rbx, [rcx + r15]        ; dest = 缓冲区 + 页号*10MiB
0x6A9A16D  mov edx, 0x100
0x6A9A179  call 0x5EE4860              ; 用 32 字节键材料设置 AES（256 位）
0x6A9A17E  mov r8d, 0x2800             ; 10240 字节
0x6A9A18E  call 0x5EE4320              ; 对页首 0x2800 字节做解密
...
0x6A9A268  add r12, 0x20               ; 每页消耗 32 字节键材料
0x6A9A26C  add r15, 0xA00000           ; 下一页 = +10 MiB
```

`0x5EE4320` → `0x73720D0` → `0x73A1B00`：16 字节分组、与前一块异或、对每块调用
回调 —— 标准 **CBC** 结构；`0x7370F00` 内是 AES 密钥扩展（S 盒表位于
`0x9C9BB50`/`0x9C9C750`/`0x9C9CB50`/`0x9C9CF50`/`0x9C9D350`，OpenSSL 风格
T 表）。每页密钥长度为 256 位（`mov [rcx+0xF4], 0x100`）。

因此新格式的物理布局为：

```
每个 10 MiB 页:
  [0x0000, 0x2800)   AES-CBC 加密的「页首块」（10240 字节 = 640×16）
  [0x2800, 10 MiB)   明文（文件表 / 各段密文直接落在逻辑流上）
```

页块与逻辑流**重叠**（就地解密），而非插入：头部（LCG 加密的 48 字节）
位于页 0 块内偏移 0，文件表从逻辑 0x30 开始 → 物理 0x30 之后是加密块、
物理 0x2808 之后是明文记录，两者栅格一致。

### 2.4 尚未解决：每页 32 字节键材料的来源

- 客户端在分页循环前先从流里读出「每页 32 字节」的键表（`0x5EE40B0` 包装的
  EVP 解密流，块大小 `[obj+0x160]`），循环中 `add r12, 0x20` 取下一页的键。
- `sk.dat` 1,920 字节 = **60 × 32**，很可疑就是这张键表（52 页只需 1664 字节）。
- 但用 `sk.dat` 的所有 32 字节窗口（以及文件头/尾窗口）按 AES-128/256 的
  CBC/ECB/CTR/CFB/OFB、IV ∈ {0, key[:16], key[16:32]} 解页 0 块，再按
  `hEAd` 等候选 LCG 种子二次解密查 `nkpi`，均未命中。
- 客户端侧的密钥对象表在 `.rdata`（如 `0x9A1CC50`、`0x9A1CBD8`、
  `0x9A1CC20`、`0x9A1CC38`、`0x9A27F60`、`0x9A27F78`，访问器
  `0x5ED0E10` 校验首 2 字节 `0x2CA1` 后返回 `obj+4`），但**静态文件中
  这些字节已被 Themida 加密**（首 u16 读到的是 `0x5400` 而非 `0x2CA1`），
  无法直接读出。

## 2.5 客户端目录：`D:\Games\dxf\110US\DFO_2.31.1.117`

- `Script.pvf` 与 `testdata/110US.pvf` **MD5 完全相同**（`754f787f…`），
  `sk.dat` 与 `testdata/sk.dat` 完全相同（`e7116fa6…`）。
- 目录里有 `110US补丁说明/`（基础汉化补丁清单）：补丁**成套**替换
  `Script.pvf`、`sk.dat`、`dstr.dat`、`ChineseLocalization.dll` 四个文件，
  并明确提到验证项包含「字符串池前缀、**索引保护**」。
  因此 `sk.dat` 是官方客户端包内文件（`localpackage.lst` 里也有），
  且与本 PVF 的「索引保护」配对。
- `ChineseLocalization.dll` 只导入 USER32/GDI32/KERNEL32（字体/文本 hook），
  不含文件与密码学代码；页块解密在 `DFO.exe` 内。
- 客户端代码路径：`0x6A99960` 打开**两个文件**（第一个读入内存做页解密目标，
  第二个整体读入后包装成流，用 `0x5EE40B0` 读出「每页 32 字节」键表），
  然后按上面的分页循环逐页 AES-CBC 解密。
- 已尝试但**未命中**的页键来源（都做了「解出的头部必须是合法 PVF 头」的校验）：
  `sk.dat` / `dstr.dat` / `NGClient64.aes` 的任意 16/24/32 字节窗口，
  AES-128/192/256 × CBC(IV=0/键内 IV/密文首块)/ECB/CTR/CFB/OFB，
  以及「先用 LCG（名表 1785 万个 4 字符名反查种子）解一层再解 AES」的组合。
  对 `sk.dat` 全部窗口做 AES 解密后的熵检测也完全是随机（≈7.98），
  说明键表本身很可能还被**另一层固定密钥**加密（该密钥位于 Themida 保护的
  `DFO.exe` 数据里）。

## 2.6 顺带确证：`testdata/Script2.pvf` 是另一种官方新格式（godof）

`pvftools.exe`（Go）依赖公开库 `github.com/dof-dev/godof@v1.1.1`，其
`pvf_reader/header.go` 描述的格式与 `testdata/Script2.pvf` **完全吻合**：

```
int32 uuidLen; char uuid[uuidLen];
int32 version; int32 dirTreeLen; uint32 dirTreeCrc32; int32 fileCount;
byte  dirTree[dirTreeLen];   // DecryptCrc(dirTree, dirTreeCrc32) 后为条目数组
  条目: uint32 fn; uint32 pathLen; char path[pathLen]; uint32 length;
        uint32 crc32; uint32 relativeOffset;
后续: 各文件内容（每块用 DecryptCrc(data, crc32) 解密）
```

`DecryptCrc`（godof `utils/binary_helper/encrypt.go`）：以
`key = crc32 ^ 0x81A79011` 逐 dword 异或，再做 `(v<<26)|(v>>6)` 位旋转。

实测 `testdata/Script2.pvf`：uuid=`fa08bf71-4395-6a4b-a3e3-2617c9fee116`、
version=66282、dirTreeLen=98,726,484、fileCount=1,075,879，解出的前几条：

```
monster/newmonsters/new/boss/samuel/action/move.act            len=92
monster/newmonsters/anton/phase1/strongleg/.../demoniclancer_nattack3.act len=302
equipment/character/common/wrist/brac_2choro994.equ            len=342
```

即**这个 614 MB 归档现在已经有完整的解析方案**（与 110US 是两回事：
110US 顶层不是该布局，仍是「90US 骨架 + 页首块保护」）。

## 2.7 服务端：`D:\Games\dxf\110US\DFO110-0.3.6服务端`（本格式的完整实现）

服务端是 .NET 10 程序，其中 `USLocalServer.Pvf.dll`（39,936 B）**就是
110US 保护格式的实现**（`protection.json` 注记为 `Obfuscar 2.2.50` 混淆、
`server: NativeAOT`）。

类/方法（.NET 元数据，名称虽被混淆成 `A/a/B/b`，但语义命名保留）：

| 类型 | 关键成员 |
|---|---|
| `Pvf110Container` | `Unprotect`、`UnsealPageKeyTable`、`UnwrapPageKeyTable`、`DecryptPageGuards`、`MetadataKeyCandidates` |
| `PvfCipher` | `ApplyGuard`、`DeriveSeed`、`Transform`、`TransformWithSeed` |
| `PvfLayout` | `KeysFor(scheme)`、静态构造 |
| `PvfArchive` | `Open`、`OpenClient110`、`OpenClient110Bytes`、`DecodeChunk`、`ReadRaw/ReadText` |
| `Pvf110KeyMaterial` | `MetadataKey`、`PageKeys`、`PagesDecrypted`、`PageKeyCount` |
| `PvfArchiveFormat` | 枚举 `Legacy*`、`LegacyNkpi`、`ProtectedNkpi`、**`Paged110`** |
| 常量 | `HeaderSize=48`、`FileItemSize=24`、`GroupItemSize=8`、`TokenSize=5`、`MagicSignature` |

已从 IL 读到的确定逻辑：

1. **头部校验**（`Pvf110Container.A` @0x273C）：取文件前 48 字节 →
   用 32 字节键做 `ApplyGuard` → `keys = PvfLayout.KeysFor(2)`（即 Paged110）
   → `PvfCipher.Transform(header48, keys.Header, 2531011)` → 要求头 4 字节
   等于 `1768975214`（`"nkpi"`）。
   注意 **seed 是常量 `2531011 = 0x269EC3`**（就是 magicMain）。
2. **页键表是 RSA 密封的**（`UnsealPageKeyTable`）：取 RSA 私钥 →
   `keySize = rsa.KeySize/8` → 校验 `sealed.Length % keySize == 0` →
   逐块 `rsa.Decrypt(block, RSAEncryptionPadding.Pkcs1)`。
   与 `sk.dat` 1920 B 相容的 RSA 尺寸：1024 位（15 块）/2560 位（6 块）/
   3072 位（5 块）等。
3. **页键表还要再用 metadata key 解一层**（`UnwrapPageKeyTable`），
   而 metadata key 是从头部字节用 `MetadataKeyCandidates` 试出来的多个候选。
4. `Pvf110KeyMaterial` 的 PEM 标记以 UTF-16 常量形式存在于 `#Blob`
   （`-----BEGIN PRIVATE KEY-----` / `-----END PRIVATE KEY-----`），
   但 **PEM 正文被 Obfuscar 的字符串隐藏加密**，静态未直接取到。

**可用性**：本机已装 .NET 运行时（`dotnet` 存在但无 SDK），`pwsh` 跑在
.NET 8/9 上，无法直接 `LoadFrom` 这个 .NET 10 程序集（
`System.Private.CoreLib, Version=10.0.0.0` 缺失）。服务端自带 .NET 10 运行时，
`GM\DNF110US-GMTool.exe` 是一个 ASP.NET Core 服务（启动后监听
`http://127.0.0.1:18110`），说明**该库确实能打开 110US 的 Script.pvf**。

### 下一阶段的两条路（择一）

- **A. 用现成实现当预言机**：装 .NET SDK，写一个几行的 C# 宿主调用
  `PvfArchive.OpenClient110(...)`，导出文件表/路径/内容；
  或直接调用 GM 工具的 HTTP API。拿到明文后即可对照实现我们的 Go 版本。
- **B. 继续静态还原**：① 解 Obfuscar 字符串隐藏得到 PEM 私钥；
  ② dump `PvfLayout.KeysFor`/`.cctor` 拿各 scheme 的分段 seed；
  ③ dump `PvfCipher.DeriveSeed/Transform/ApplyGuard` 与
  `DecryptPageGuards/UnwrapPageKeyTable` 得到完整算法，然后在 `internal/pvf`
  里实现 Paged110 变体。

## 2.8 已跑通：110US.pvf 解析成功 + 固定常量已提取

装好 .NET 10 SDK 后，我写了一个宿主程序（`C:\Users\zhyip_xb2zvud\pvfhost`，
通过 `<Reference>` 直接引用服务端那个 DLL），对
`D:\Games\dxf\110US\DFO_2.31.1.117\` 的客户端文件调用：

```csharp
var a = PvfArchive.OpenClient110(archivePath, executablePath /*DFO.exe*/, sealedKeyPath /*sk.dat*/);
```

实测结果（**文件被完整解析**）：

```
format = Paged110
fileCount  = 4,311,296        chunkCount = 58,372
bodyOffset = 178,654,053
header: bodySize = 364,769,260   groupCount = 58,372
        hashSize = 41,190,252    nameSize = 33,525,673
```

`EnumeratePaths()` 返回 4,311,296 条路径，`ReadText/ReadRaw` 能正确反编译，
例如 `appendage/creature/creature.lst`、`aradadventure/equipment/equipment_1.equ`
（`#PVF_File` 文本）都正常。

### 已提取的固定常量（Go 复刻所需）

| 常量 | 值 |
|---|---|
| `Pvf110Container.PageSize` | 10485760 |
| `PageGuardSize` | 10240 |
| `PageKeySize` | 32 |
| `MetadataAlignment` | 256 |
| `PvfCipher.DataConstant` | 2531011 = `0x269EC3` |
| `PvfCipher.StringConstant` | 2531017 = `0x269EC9` |
| `PvfCipher` LCG 乘数 | 214013 = `0x343FD` |
| PEM 标记 | `-----BEGIN PRIVATE KEY-----` / `-----END PRIVATE KEY-----` |

**RSA 私钥**（RSA-1024，位于 `DFO.exe` 内，由 `Pvf110Container` 解析）：

```
Modulus = B3120BBA28E9DB7A87BF61EA60FDD281…394F
D       = 9ACDEEF570893B04227680DF6E19FFF1…F701
P       = EA47490BB1F9402393A621B0C1389EAC…3589
Q       = C3AC5F46B88CD24538CABE7EA551DB33…9A17
DP/DQ/InverseQ 亦已导出（见 rsa_pkcs1.der）
```

`sk.dat`（1920 B）经 `rsa.Decrypt(block, Pkcs1)` 逐 128 字节块解封后得到
**1664 B = 52 × 32 的页密钥表**（正好对应 52 个 10 MiB 页）；再经
`UnwrapPageKeyTable(table, metadataKey)` 用 metadata key 剥一层。
`MetadataKeyCandidates(DFO.exe)` 返回 3 个 32 字节候选。

### 分段的 LCG 密钥名（关键收获）

`PvfLayout.KeysFor(scheme)` 给出三套 scheme 的字符串密钥：

| scheme | Header | GroupTable | AnsiPool | UnicodePool | BodyChunk |
|---|---|---|---|---|---|
| LegacyNkpi (0) | `HeaD` | `GRPI` | `sTrA` | `sTrW` | `BodY` |
| ProtectedNkpi (1) | `hEAd` | `grpi` | `StRa` | `StRw` | `bODy` |
| **Paged110 (2)** | **`iNfO`** | **`Gidx`** | **`stAs`** | **`stWs`** | **`mAIn`** |

配合从 DFO.exe 反解出的宽字符种子公式
`seed = 0x339E9711*u0 + 0x393*(u3 + 0x393*(u2 + 0x393*u1))`，
即可算出 Paged110 各段 seed。池长度掩码：Legacy/Protected 为
`2859747118 / 2592272439`，Paged110 为 `3886938090 / 3101599660`。

### 尚未还原完整算法、但已可用的部分

`UnwrapPageKeyTable` 的剥壳规则、`DecryptPageGuards` 与
`ApplyGuard`/`Transform` 的细节还没逐条落到 Go；因为宿主已经能直接用，
**读取不受影响**。下一步照 §2.9 在 `internal/pvf` 里实现 Paged110 变体。

## 2.9 已在 Go 中实现（`internal/pvf/paged110.go`）

用宿主程序取测试向量逐条反推后，Paged110 的完整算法已确证并移植到本项目：

| 步骤 | 结论 |
|---|---|
| 页密钥表解封 | `sk.dat` 按 128 字节块做 RSA-1024 PKCS#1 v1.5 解密 → 1664 B（52 页 × 32 B） |
| 二次解包 | `AES-256-CBC(IV=0)`，作用于前 `len & ~0xFF` 字节，尾部 128 B 不动 |
| metadata key | `DFO.exe` 中 64 个十六进制字符的一串（本客户端第 3 段 `21ad8ff2…4489`），已内置 |
| 页首块 | `AES-256-CBC(IV=0)`，密钥 = 表内第 i 个 32 字节 |
| 头部 | 无 `0x55` guard；`seed("iNfO") = 0x1AAEB306`，magic `0x269EC3` → 4 字节 `nkpi` |
| 分段密钥 | `iNfO`/`Gidx`/`stAs`/`stWs`/`mAIn`，宽字符公式；池掩码 `3886938090`/`3101599660` |

实现细节：

- `internal/pvf/paged110.go`：常量、`wideSeed`、`paged110Keys`、内置 RSA 私钥、
  解封/解包/页首块解密、metadata key 候选扫描与解锁。
- `internal/pvf/archive.go`：`Open` 把归档所在目录传给 `parse`，标准/变体路径都
  失败时尝试 Paged110；`Parse`（纯内存）不尝试。
- `internal/pvf/stringpool.go` + `variant.go`：`keySet` 增加 `maskA/maskW`，
  名称池掩码随方案变化。
- `internal/pvf/save.go`：Paged110 **写回已实现** —— 打开时把解包后的页密钥表留在
  `Archive.pageKeys`，写出时按页重新加密前 10,240 字节（`writePageGuarded` 流式写）；
  未修改的归档重新写出逐字节等于原文件。只支持就地修改现有文件内容：增删文件会
  重建名称池而 HASH 段密钥未知（只能原样搬运），因此返回
  `ErrPaged110StructureLocked`。
- 测试 `internal/pvf/paged110_test.go`：对 `testdata/110US.pvf` 端到端解析
  （4,311,296 条、58,372 块、路径可解析、脚本可读）、常量与密钥管线校验、
  **未修改保存的 MD5 与原文件一致**、以及「改一条 `.str` 条目 → 保存 → 重开」
  的往返测试。

性能：`Open` 只读 `sk.dat`，内置 metadata key 命中时**不会**去读 212 MB 的
`DFO.exe`；全部页首块解密约 53 万字节 AES，可忽略。实测打开 543 MB 归档
约 6–7 秒（与读取文件本身同量级）。

## 2.10 新版本的 token 类型 8 / 10（equ/stk 的 name 字段）

Paged110 归档的脚本 token 流除了 0/2/3/5/6/7，还用了两种字符串池引用 token:

| type | 含义 | 渲染 |
|---|---|---|
| 8 | 字符串池引用 | `{8=\`...\`}`，例如 `[name]` → `{8=\`<31::equip_name_1>\`}` |
| 10 | 字符串池引用(多行命令文本) | `{10=\`...\`}`，例如 `.act` 里的 ` rs(...) ` |

旧实现只处理 5/6/7，因此 8/10 被直接丢弃 —— 表现为 `.equ`/`.stk` 的
`[name]`/`[flavor text]` 渲染成空。已修正的位置：

- `internal/pvf/file.go`：渲染分支并入 `5, 7, 8, 10`（用 `{N=\`...\`}` 通用形式）、
  `scriptTokenValue` 把 8/10 当池引用解析。
- `internal/pvf/script.go` / `script_view.go`：`{8=…}` / `{10=…}` 标记可被解析回
  type 8/10（原先只认 `{5=`/`{7=`，且硬编码 3 字符前缀）。
- `internal/pvf/batch.go`、`script_document.go`：补 8/10 的名称与
  `ScriptTokenBlock8` / `ScriptTokenBlock10` 类型。

顺带修掉一个与格式无关、但会影响编辑保真的旧问题：整数值的**浮点** token
（如 `8000.0f`）过去渲染成 `8000`，再编码就变成整数 token。现在整值浮点写成
`8000.0`（`formatScriptFloat`），token 流可逐字节往返。

> 说明：`<31::equip_name_1>` 这类占位符是客户端运行时按字符串表解析的
> （本归档里对应 `list/n_string.lst`、`n_string_kor.lst` 与 `string/*.str`），
> 参考实现(pvfUtility/服务端库)渲染的也是占位符本身。本项目额外实现了一层
> 只读的占位符解析，见 §2.11。

## 2.11 占位符解析（`<table::key>` → 实际文本）

客户端把物品名放在独立的字符串表里，脚本中只留 `<表号::键名>` 占位符。
解析链路（`internal/pvf/stringtable.go`）：

1. `stringTableFiles` 按**优先级递减**列出映射文件：
   `list/n_string.lst`（基准）、`n_string.lst`、
   `list/n_string_translate.lst`、`n_string_translate.lst`、
   `list/n_string_kor.lst`、`n_string_kor.lst`。
   **基准表必须排在最前**：`list/n_string.lst`（37 项）指向 `String/*.uv.str`，
   里面是客户端自己显示的语言（这份 110US 里是中文/英文：`equipment.uv.str`
   62 万条键）；`*.kor.str` / `*.translate.str`（只映射 3 和 13 两个表号）是
   随包的语言覆盖层。把覆盖层排在前面会**在基准表有中文时也显示韩文**，这正是
   早期版本的现象。
2. `.lst` 本身是普通脚本 token 流：`整数` + `字符串` 交替，前者是表号，
   后者是该表的 `.str` 路径。
3. `.str` 是 UTF-16LE 文本，每行 `键>值`，`//` 为注释；文件缺失时回退到同名
   的 `.translate.str` / `.kor.str` / `.uv.str` 变体。
4. `ResolveStringTable(表号, 键)` 从基准表往下找**第一个非空**值，
   同时回报来源路径与是否回退（`Fallback`）。**空值不算命中**：基准表用
   `键=` 的写法列出了所有已知键，其中装备名有 36.7%（133,487/363,315）是空值
   （中文补丁没翻），若把空值当命中，这些名字会被直接抹掉。
5. `ResolvePlaceholders` 替换文本中出现的所有 `<表号::键名>`；**解析不到时
   原样保留**，不丢信息。`ResolvePlaceholdersMarked(text, suffix)` 会给出覆盖层
   的取值追加 `suffix`，供界面标注「未翻译」。

暴露的 API：`Archive.ResolveStringTable`（带来源）、`Archive.LookupStringTable`、
`Archive.ResolvePlaceholder(s)` / `ResolvePlaceholdersMarked`、
`Archive.ItemName(i)`（`[name]` 段 + 占位符解析）。

消费方：`services/equipment_preview.go` 的 `ParseEQU` 在构建完 `[name]`/`[name2]`
后调用 `resolvePreviewText`，显示解析后的文本；如果该名只由语言覆盖层答出，
会追加 `（未翻译）` 标记（如 `포니 비즈 뱅글[A타입]（未翻译）`）。
解析是**纯展示层**的：归档里存的就是占位符，脚本文本不会被改写。

实测 110US：抽查 40 个含占位符的 `.equ`/`.stk`，仅 1 个未命中
（`aradadventure/equipment/equipment_1.equ` 的 `<31::equip_name_1>`：该表的
`.str` 里确实没有这个键），测试因此按 ≥90% 的比例断言。解析结果示例：
`白色兽语腰带 [A款]`、`海军风情远航长剑`、`The White Beast Fox Ears [Type A]`，
以及基准表优先生效的例子 `equipment/character/archer/avatar/belt/117530002.equ`
→ `稀有克隆装扮腰部`（韩文表里同键是 `레어 허리 클론 아바타`）。

抽查前 2000 个带占位符的 `.equ`：中文 27%、英文 34%、韩文 39%
（韩文全部来自基准表空值后的覆盖层回退，界面上都带 `（未翻译）` 标记）。

> `.str` 解码目前不做 CP437→EUC-KR 修补（`fixKoreanMojibake`）；这份归档的
> `.str` 本身就是正常的 UTF-16 韩文/中文，加修补反而有把正常文本改坏的风险。
> 若遇到韩服转制的归档出现 `╟╤` 这类乱码，可在 `readStringTable` 里按行接上该修补。

## 2.12 110US 的列表布局（`list/*.lst`）与索引

110US 不只换了容器和字符串表，**列表文件的布局也变了**：

| | 90US（`Script.pvf`） | 110US（Paged110） |
|---|---|---|
| 装备列表 | `equipment/equipment.lst` | `list/equipment.lst` |
| 列表条目 | 相对列表所在目录（`character/a.equ`） | **相对归档根**（`equipment/character/a.equ`） |
| 其它列表 | `stackable/stackable.lst`、`npc/npc.lst`、… | `list/stackable.lst`、`list/npc.lst`、… 全部集中在 `list/` |
| 技能列表 | `skill/swordmanskill.lst` | 同左（未变） |

归档里共 566 个 `.lst`，其中 37 个在 `list/` 下（`list/n_string.lst` 等字符串表映射
也在这里）。因此**项目里 `config/lists.json` 配的 90US 路径在 110US 上一个都找不到**，
表现就是：搜索索引里没有装备/道具记录、树节点没有名称标签、只能按路径搜。

实现（两条都做，新旧归档通用）：

- `Archive.FindList(path)`：先按配置路径找，再退到 `list/<文件名>`，
  反向也成立（配了 `list/x.lst` 时会退到 `<x>/x.lst`）。
  搜索索引、`FindFileRegistrations`、NPC 名单都改走它。
- `listPathCandidates` 增加「按归档根解释条目」的候选（`(r)` 变体同样两种都试）：
  先按列表目录解释（90US），再按归档根解释（110US）。

配套的 kernel 修正：`ScriptMetadata`（搜索/树名称的来源）原先只认 token 类型
3/5/6/7，110US 的 `[name]` 是 type 8/10，于是**名称一直是空的**；现在 8/10 与旧类型
同等处理，并且取到的名称会做占位符解析，所以索引里存的是
`白色兽语腰带 [A款]` 这样的显示文本而不是 `<3::name_514530375>`。

实测 110US：`FindList("equipment/equipment.lst")` → `list/equipment.lst`（361,348 条），
`FindList("stackable/stackable.lst")` → `list/stackable.lst`（139,533 条）；
索引共 638,542 条记录（跳过 969），构建约 **21 秒**（打开归档约 7 秒另计）；
搜索 `白色兽语腰带`、`海军风情远航长剑`、`The White Beast Fox Ears` 均能命中
（中英文都可搜）。

**未翻译标记**：`ScriptMetadata` 除了给出解析后的名称，还回报 `NameFallback`
（该名只由语言覆盖层答出）。`services` 层的 `markedName` 给这种名字追加
`（未翻译）`，因此**文件树标签、搜索结果、装备悬浮预览、编辑器标注**四处一致：

```
[equipment] 포니 비즈 뱅글[A타입]（未翻译） | equipment/character/archer/avatar/belt/117530006.equ
[equipment] 白色兽语腰带 [A款]              | equipment/character/demoniclancer/avatar/belt/514530375.equ
```

## 2.13 编辑器里显示译文，并就地修改

编辑器**不能**把占位符替换成译文再保存 —— 那会把归档里的数据改掉（占位符本身
才是存储内容）。因此译文以「标注标签」的形式挂在占位符后面，并且可以直接改：

- `services/annotations.go` 的 `appendPlaceholderAnnotationsLocked` 扫描脚本视图里
  的 token，凡是 `<表号::键名>` 能解析出来的，就追加一条
  `type = "placeholder"` 的 `EditorAnnotation`：`Title` 是译文（覆盖层答出的加
  `（未翻译）`），tooltip 里给出原始占位符与来源 `.str`，并带上结构化的
  `Placeholder{TableIndex, Key, Fallback}`，供界面直接发起修改。
- 来源 `.str` 若没超过编辑器 8 MB 上限，标签还带 `TargetFileIndex`，
  **Cmd/Ctrl+单击可直接打开该字符串表**。
- 文档本身仍是原样的占位符，编辑/保存的字节不受影响；标签位置随编辑自动跟随
  （复用 annotation 的 `StateField` 位置映射）。
- 前端样式：`cm-annotation-tag--placeholder`（斜体 + 虚线边框）与
  `--editable`（可点、悬停下划线）。
- **改译文**：单击标签 → 弹框（预填当前译文）→ 确定 → 走
  `EditorService.SetPlaceholderText(脚本 index, 表号, 键名, 新文本)`
  → `core.setPlaceholderText` → `Archive.SetStringTableEntryAt`。
  实现要点：
  - 目标表取「当前真正答出这个键的那一张」（可能是韩文/翻译覆盖层，而不是基准表）；
    没有任何表有这个键时，追加到基准表末尾（用该表主流的行尾符）。
  - **按字节就地替换**：在 UTF-16LE 载荷里找 `键>` 所在行，只换值的那段字节，
    BOM、行尾符（`\r\n` / `\n`）与其余字节一个不动；解析不了的表直接拒绝写入。
  - 改完立刻刷新：编辑器标注（标题变成新译文）、文件树标签、搜索索引里该文件的
    名称（`refreshIndexedRecordsLocked`），并记入版本/撤销历史
    （`recordVersionMutationLocked("编辑字符串表", …)`）。
  - 真实归档测试：`TestSearchIndexRealPaged110` 在 110US 上改
    `name_117530002` → `.str` 更新、脚本文本不变、树标签与新名字可搜。
- 注意：改动落在**内存 overlay**，要点「保存」才会写回归档；110US 的保存依赖
  §2.9 的页首块回加密（已实现），且只支持就地修改（不能增删文件）。

### 2.13.1 新建文件时怎么写名称

110US 的物品名基本都放在字符串表里（实测抽 368 个 `.equ`/`.stk`：365 个是
`<表号::键名>` 引用、0 个内联字面量），所以**新文件也应该写引用**。完整配方：

1. **建文件**：新建 `xxx.equ` / `xxx.stk`。
2. **写引用**：在 `[name]` 段写 `{8=`<3::你的键名>`}`。
   注意花括号里的 **8 是 token 类型**（字符串池引用），**表号在 `<表号::键名>` 里**
   （装备=3、道具=13 最常用）。不要写成裸的 `<3::键名>`：那样会被编码成 type 3
   标签串，不是名字。
   - 手写嫌麻烦：编辑器工具栏「**字符串引用**」按钮会弹框问表号/键名/译文，
     确定后自动在光标处插入 `{8=`<表号::键名>`}` 并把条目写进对应字符串表。
   - 只写了引用、还没建条目时，标签会显示红色的「未定义」，点它就能直接填译文
     （后端把新键追加到该表号的基准表，插在尾部 NUL 填充之前）。
3. **注册进列表**：把 `id 路径` 加进 `list/equipment.lst`（道具是
   `list/stackable.lst`）。`.lst` 是普通脚本，可以直接编辑文本，或用脚本工作区的
   `pvf.lst("list/equipment.lst").set(id, path)`（361k 条的大表不适合手改）。
4. **保存**：结构性改动会重建名称池与 HASH（110US 约 35 秒），之后重开即可在
   树/搜索里看到新名字。

也可以沿用 90US 的写法直接把名字内联（`` `新装备名` ``）：解析层同样认（
`ScriptMetadata` 支持 type 5/6/7 与 8/10），但这个客户端的文件 100% 用引用，
建议保持一致。

## 2.14 与 90US 的字符串方案对比

实测对象：`testdata/Script.pvf`（90US，CN 客户端，105 万文件）与
`testdata/110US.pvf`（Paged110，431 万文件）。

| | 90US | 110US |
|---|---|---|
| 映射文件 | 归档根 `n_string.lst`（30 项）+ `n_string_kor.lst` + `n_string_translate.lst` | `list/n_string.lst`（37 项）+ `list/n_string_kor.lst` + `list/n_string_translate.lst`（根目录同名文件也还在） |
| 基准表指向 | `Character/Character.chn.str`、`equipment/equipment.chn.str` … | `String/*.uv.str` |
| `.str` 实际数量 | **20** 个（chn 10 / kor 7 / jpn 3），最大 648 KB | **84** 个（uv 39 / kor 40 / translate 2），`string/equipment.uv.str` **51 MB**、`string/stackable.uv.str` 40 MB |
| 覆盖范围 | 只有 `aicharacter/`、`etc/`、`monster/`、`passiveobject/`；`equipment`、`character`、`dungeon`、`itemshop`、`map` 的 `.str` **在该客户端里根本不存在**（`n_string.lst` 里却列了） | 全部实体：item / character / dungeon / map / monster / npc / passiveobject / … |
| 脚本里的显示文本 | **内联字面量**：`` [name] `羽林将军的黑色头饰` ``（token type 6） | **占位符**：`[name]` → `{8=`<3::name_10018>`}`（token type 8/10） |
| 抽样各 200 个文件的占位符比例 | `.equ` 0、`.stk` 0、`.mob` 0、`.obj` 0 | `.equ` 200、`.stk` 200、`.mob` 198、`.npc` 197、`.obj` 133、`.act` 65 |
| 表号语义 | —（不存在表引用） | `<数字::键名>`，数字是 `n_string.lst` 里的表号 |
| 键名风格 | —（表里也是 `name_<ID>` 这类，但物品表不存在） | `name_<物品ID>`、`basic_explain_<ID>`、`flavor_text_<ID>` … |
| 文件格式 | UTF-16LE、每行 `键>值`、`//` 注释；`.lst` 是 `整数 + 字符串` token 流 | **与 90US 完全相同** |

要点：**格式没变，用法和规模变了**。90US 已经有字符串表机制（`.lst` 映射 + UTF-16
`.str`），但那份客户端只带了 20 个 `.str`（怪物名、被动对象、角色台词、etc 文本），
物品/角色/地下城等名称直接内联在脚本里；110US 把这些名称整体搬进 `.str`，脚本里
只留数字表号占位符，并新增 type 8/10 字符串池引用 token 承载它们。

两个容易混淆的点：

- 90US 的 `<npc::1071>`、`<apc::26514>` **不是字符串占位符**，而是对话文本里的
  「说话人/立绘引用」标记，后面还跟着真正的文本（`` `<npc::1071>卢克爷爷是个很伟大的人！` ``）。
  实现里 `parsePlaceholder` 只接受数字表号，所以不会误解析它们。
- 90US 那份 CN 客户端的 `.str` 数据质量差：`etc/etc.chn.str` 里非 ASCII 大量变成
  `?`，`monster/monster.kor.str` 是「韩文逐字替换成形近汉字」的产物
  （如 `agamemnon_01>堡戎狼 酒啊糕稠`）。110US 是正常中文/英文/韩文。
  两者的 `.str` 都没有 CP437 伪画（`looksLikeCP437Painting` 为 false），
  所以读取路径不需要韩服乱码修补。

容器侧另有一处相关差异：字符串池的 section 名与长度掩码不同 —— 90US 是
`sTrA`/`sTrW`（掩码 2859747118 / 2592272439），110US 是 `stAs`/`stWs`
（掩码 3886938090 / 3101599660，见 §2.9）。

## 2.15 HASH 段密钥：反解与结果

HASH 段布局（`count + count×(nameOff,pathOff) + n + n×偏移表`，见 FORMAT §8）之前
只在标准 90US 有密钥，Protected 变体与 Paged110 都「原样搬运」，导致增删文件时
HASH 会指向失效偏移。这次做完了：

**Protected 变体：密钥名就是 `hash`（全小写）+ `wideSeed` + magic `0x269EC3`。**
做法不是穷举（4 字符名空间太大），而是只试 16 个大小写组合 × 两种公式，
再用结构验证。实测：

| 归档 | 用 `wideSeed("hash")` 解出 | 与本地重建的对比 |
|---|---|---|
| `Script.pvf`（105.7 万文件） | count=1,057,644、n=558,373 | 条目集覆盖我们全部 1,057,625 条，客户端多 19 条（历史残留） |
| `Script3.pvf` | count=1,057,892、n=558,622 | 同上（多 19 条） |

**Paged110：密钥名未知，但种子可由结构反解。** 段首 dword 是条目数（110US 恰好等于
文件数 4,311,296），已知这个明文 dword 就钉住首个 keystream dword 的低 16 位，
每个 (magic, count) 只剩 ≤4 个候选；再用「长度精确满足 `4+count*8+4+n*4` + 抽样
偏移可解析 + 尾部按串递增」筛选。110US 解出 `seed=0x5824C072`、magic `0x269EC3`，
解出来的条目集与尾部偏移集**与我们重建的表 100% 一致**（4,311,296 / 1,674,969 全中）。
90US 变体族因为条目数比文件数多，搜索窗口取文件数 ±64。

**顺带修掉一个真 bug**：`buildNameTable` 过去硬编码 90US 的 `sTrA`/`sTrW` 密钥与
`xorStrA`/`xorStrW` 掩码。90US 两个家族的掩码恰好相同所以没暴露；Paged110 的掩码
不同，写回后段首长度字段解码成垃圾，解析器直接跳过整个池 —— 表现是重开后**所有
路径与名称变空**。现在按归档自身的 `keys.strA/strW` 与 `maskA/maskW` 写回。

**第二个真 bug（新增表项时才会碰到）**：`.str` 载荷末尾有 NUL 填充，而读取端
`decodeUTF16` 在**第一个 NUL 处截断**。原先「追加新键」是直接 append 到文件末尾，
于是新行落在填充之后，谁也看不见（同一个会话里查表就已经是 miss）。现在先把尾部
NUL 填充摘出来，插在填充**之前**，再把填充补回去；顺带支持编辑「没有行尾符的
最后一行」（遇到 NUL 即为行尾）。

验证：

- `TestPaged110StructuralEditRoundTrip`：向零售 110US **新增一个文件** → 保存
  （页首块回加密 + HASH 重新生成 + 名称池重建）→ 重开：文件数 +1、新文件可读、
  重建的 HASH 索引到新条目、原有物品名仍解析正确。
- `TestVariantHashRegeneratedOnSave`：90US 变体同上，HASH 由「逐字节搬运」改为
  「重新生成并覆盖新条目」。
- 代价：结构性编辑要重新压缩名称池（110US 的 UTF-16 池解压后 401 MB，实测约 35 s）；
  只改文件内容的编辑不触发池重建。

## 2.16 `list/*_indexhash.etc`（新增物品要写的第三个文件）

110US 的 `list/` 下每个列表都配了一个 `<列表名>_indexhash.etc`（13 个，90US 完全没有）：

| 列表 | 索引文件 | 列表条目 | 索引条目 |
|---|---|---|---|
| `list/equipment.lst` | `list/equipment_indexhash.etc` | 361,348 | 413,119 |
| `list/stackable.lst` | `list/stackable_indexhash.etc` | 139,533 | 139,754 |
| `list/monster.lst` | `list/monster_indexhash.etc` | 13,731 | 16,751 |
| `list/npc.lst` | `list/npc_indexhash.etc` | 3,361 | 3,977 |
| `list/appendage.lst` | `list/appendage_indexhash.etc`（另有 `_v5.etc`） | 7,843 | 8,345 |

**结构（已确认）**：和 `.lst` 同一套 token 流，`(type 0 整数 id)(type 6 字符串)`，
每条 10 字节。第二个 token 不是 int，而是**字符串**；字符串内容是**十进制数字**
（一个 uint32；因为可能超过 2^31，所以没有用有符号 int token 存）。

**这个数字是什么（已排除的假设）**：

- 不是路径/名字的常见哈希：crc32(IEEE/Castagnoli/Koopman)、fnv1/fnv1a、djb2、sdbm、
  java、jenkins、bkdr、LCG 系列（用容器自己的 `0x343FD/0x269EC3` 等）等 ~30 种函数
  × 路径/目录/文件名/去扩展名/小写/id 文本/`name_<id>`/UTF-16 字节 ≈ 0 命中（每项 400–2000 样本）。
- 不是路径相关：`stackable` 与 `appendage_v5` 共有 3160 个 id，**路径完全不同但数值完全相同**。
- 不是 id 的线性函数（`A*id+B` 只在前两条上成立）；也不是另一条记录的 token/偏移。
- 它只跟 **id** 有关：`stackable` / `monster` / `dnf` / `appendage` / `appendage_v5`
  之间共有 163–6094 个 id，数值 100% 相同（`equipment` 的 id 是真实物品 ID，与这些
  “序数 id”列表没有可比样本）。`id=0 → 0`。

**覆盖率**：索引文件是列表的**严格超集** —— 五组样本里「在列表里但索引没有」的
条目都是 **0**，而多余条目（历史残留）有几百到 5 万。也就是说：原厂工具每次维护列表时
都会顺带把索引补齐，而**我们自己新增的 id 就会缺这一条** —— 这正是「新增道具失败」
最可疑的地方，但我们无法凭空算出那个数字。

**已实现的工具**（`internal/pvf/indexhash.go`）：

- `IndexHashCompanionPath(listPath)`：`list/x.lst` → `list/x_indexhash.etc`。
- `IndexHashPairs(path)`：解析成 `{ID, Value}`。
- `IndexHashGaps(listPath)`：**列出「在列表里、但索引里没有」的 id**（新增物品后
  这里就会出现你新加的 id）。
- `SetIndexHashEntry(path, id, value)`：按原文件的写法追加/更新一条
  （值写成十进制文本再取字符串池偏移，其余条目逐字节保留）。
- `IndexHashSiblingPaths(listPath)`：附带 `_v5` 变体时一并列出。
- 测试：`TestIndexHashReadWrite`（读写/更新/追加/字节结构）与
  `TestIndexHashRealArchive`（零售 110US：两个列表的 gaps 都是 0；注册一个新 id
  后 gaps 恰好是那个 id，写入后归零）。

**还没解决**：那个 uint32 的生成函数。要对上新条目，需要下面任一条件：

1. 在游戏里试验：先只加 `.lst`（现状）→ 看物品是否出现；再把索引条目用
   邻近条目的数值补上 → 再看。这能判定客户端是否校验这个值。
2. 找到写这批 `.etc` 的工具（本机 `D:\Games\dxf\tools` 里的工具与 `DFO.exe`、
   服务端二进制都没有 "indexhash" 字样，客户端也不含 `n_string`/`equipment.lst`
   这类字面量，所以文件名是运行时拼的，无法用字符串搜索定位）。
3. 另一份同版本客户端的同名文件：若同一个 **新 id** 在别处已有条目，就能直接读出
   正确值甚至拟合函数。

## 3. 复现用脚本（当时临时创建，已删除）

分析时用过：

- 用 `capstone` 反汇编 PE 指定 RVA（按 `.pdata` 取函数边界）；
- 用穷举 4 字符密钥名 + `newKeySeed` 匹配已知种子；
- 用 `pycryptodome` 尝试 AES 键/模式组合。

如需继续，建议保留其中「PE 反汇编 + 新公式穷举」两步。

## 4. 建议的后续步骤

1. ~~补 HASH 段的密钥名~~ **已完成**，见 §2.15。
2. **`Script2.pvf`（godof 布局）**：按 §2.6 实现独立解析路径，
   项目里的 614 MB 归档即可打开（可用 `dof-dev/godof` 作为对照实现）。
3. **字符串的编辑体验**（承接 §2.14）：**已完成** —— 编辑器里点占位符标签弹框改译文，
   后端按 UTF-16LE 字节就地替换对应 `.str` 条目（详见 §2.13），保存后生效。
   大表（`String/equipment.uv.str` 48.9 MB）不需要整文件打开也能改单条。
4. **界面消费占位符解析**：装备预览、编辑器标注、搜索结果与文件树标签都已接入；
   其余面板（批次处理、版本对比里的名称）如需显示译文再复用
   `ResolvePlaceholders` / `markedName` 即可。

### 历史备注（已完成）

- 110US 的页键：由 `sk.dat` → RSA-1024 PKCS#1 v1.5 → AES-256-CBC(IV=0) 解出，
  见 §2.8；不需要借助第三方工具或 dump 进程内存。
- 110US 的写回：页首块在保存时重新加密，见 §2.9；未修改归档逐字节还原。
- HASH 段密钥：Protected 变体是 `wideSeed("hash")`，Paged110 由结构反解，
  见 §2.15；增删文件因此可用。
