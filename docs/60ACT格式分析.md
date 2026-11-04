# 60ACT 归档读写

本地 A1、A4、A5、A12 样本均已通过目录与全部文件 CRC 校验。它们不使用分页保护，不需要客户端 EXE 或 sk.dat。

| 变体 | 文件数 | 文件头 | 目录项顺序 | 文件 CRC 初始值 |
| --- | ---: | --- | --- | --- |
| A1 | 48,326 | `DNF_SCRIPT_PACK`（15 字节） | 编号、偏移、长度、CRC、路径长度、路径 | 文件偏移 |
| A4 | 71,019 | UUID 长度、UUID、版本 | 编号、路径长度、路径、长度、CRC、偏移 | 文件编号 |
| A5 | 126,696 | 同 A4 | 同 A4 | 文件编号 |
| A12 | 145,184 | 同 A4 | 同 A4 | 文件编号 |

A1 的签名之后，或其余版本的版本号之后，均为三个小端 uint32：目录密文长度、目录 CRC、文件数。目录后紧接文件数据区，目录中的文件偏移相对于此数据区。

目录和文件均按四字节对齐。逐 uint32 解密为 `ROR32(cipher XOR CRC, 6)`。目录 CRC 为 `crc32.Update(文件数, IEEE, 对齐后的目录明文)`；文件 CRC 使用上表初始值，对齐后的文件明文参与计算，读取时再截断到声明长度。现有 TW 容器的额外异或常量 `0x81A79011` 不适用于这四个样本。

文本以国服 GBK 解码，保留注释和排版；有 UTF-8 BOM 时按 UTF-8 读取。语言后缀不用于推断编码。路径按既有 CP949 路径约定读取。带 NUL 的原生资源保留为二进制，不尝试解释成五字节脚本令牌。A5/A12 大多数 ANI 为二进制，可通过通用 ANI 模块解码为只读文本并预览，原始载荷保持不变。

支持读取、搜索文本、修改、增删文件及保存。磁盘格式没有字符串池或 `stringtable.bin`；打开时为文本脚本建立编辑器内部的令牌视图和字符串池，统一作为 `TypeScript` 使用，兼容名称、稀有度、图标、列表编号、标注、GUI 和批量脚本解析。`.str`、`.txt`、`.nut` 等普通文本仍为 `TypeUnicode`；含 NUL 的二进制资源保持原样。源码支持正负号数字、嵌套 Section、反引号字符串、`//`/`/* */` 注释和 `<table::key>` 本地化引用，兼容 ACT60 根目录的 `string.lst` 字符串表映射。未修改的归档逐字节原样导出；修改后重建目录与文件数据区，保留原始签名、UUID、版本和已有文件编号。未修改文件不经过字符解码再编码，保持原始载荷和对齐字节。A1 文件移动时以新偏移重新计算 CRC 并加密，其他版本沿用未修改文件的原始密文。

文本修改按原编码保存（默认 GBK，原有 UTF-8 BOM 文件继续使用 UTF-8 BOM），不能用 GBK 表示的字符会报错并保留待保存修改。新增文件路径按 CP949 编码。导入的令牌脚本在保存时转为文本源码，重新打开后继续提供 `TypeScript` 语义视图。直接文本编辑保留原始排版与注释，仅修改注释或空白也会被记录；GUI/令牌编辑按脚本渲染规则输出源码，内部块字符串标记不会写入原生 ACT60 文本。二进制 ANI 可原样保留或替换原始载荷，支持只读文本解码与预览，尚不支持文本重编译。保存前重新解析并校验所有文件，写入失败时不清除内存修改。

文本导入（含 MCP）在识别 Unicode BOM、无 BOM UTF-16 和有效 UTF-8 后，优先尝试当前档案编码（ACT60 自动模式为 GBK）。只有解码后重新编码能逐字节还原来源时才采用；否则回退统计探测。这样可避免短文本及 `itemshop/magic2.shp` 这类同时兼容 GBK/EUC-KR 的文件被误判为韩文，随后无法用 GBK 保存。

实现位于 `internal/pvf/act60.go` 和 `internal/pvf/act60_save.go`，合成回归覆盖两种目录布局、GBK 文本、二进制、空文件、克隆、比较、原样导出、编辑、增删、连续保存、UTF-8 BOM 保留、编码/写入失败重试与损坏/截断数据。真实样本验证使用：

```sh
PVF_TESTFILE=/path/to/Script.pvf go test ./internal/pvf -run '^TestRealACT60(Parse|EditRoundTrip|AmbiguousShopEditRoundTrip)$' -v -count=1
```

导入预览、普通导入和 MCP 文本读取的真实样本回归：

```sh
PVF_ACT60_TESTFILE=/path/to/Script.pvf go test ./services -run '^TestRealACT60ImportNativeEncoding$' -v -count=1
```

## pvfUtility 对照

核对版本 `53dc8e48bba0f7871ca96bcf10bfe6d2028d0582`：其容器读取固定使用 TW 异或常量，没有上述 ACT60 容器分支，因此无法直接打开这些样本。但其 `BinaryAniCompiler` 支持二进制 ANI，后续可参考其格式分析：

- [容器读取](https://github.com/Lancarus/pvfUtility/blob/53dc8e48bba0f7871ca96bcf10bfe6d2028d0582/SourceLibraries/PvfCode.Services/PvfCode/PvfGroup.cs#L188)
- [加解密算法](https://github.com/Lancarus/pvfUtility/blob/53dc8e48bba0f7871ca96bcf10bfe6d2028d0582/SourceLibraries/PvfCode.Models/PvfCode/Models/Pvf/PvfAlgorithmHelper.cs#L49)
- [二进制 ANI](https://github.com/Lancarus/pvfUtility/blob/53dc8e48bba0f7871ca96bcf10bfe6d2028d0582/SourceLibraries/PvfCode.Services/PvfCode/Services/BinaryAniCompiler.cs#L166)
