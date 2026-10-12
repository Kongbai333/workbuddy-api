# Windows 图标与 exe 资源

`dist/workbuddy-api.exe` 带图标和版本信息（资源管理器属性里能看到产品名）。
这些资源来自 `cmd/workbuddy-api/rsrc_windows_amd64.syso`，由本目录两个脚本从
`windows/icon.ico` 生成。

## 用法

```bash
# 1. 改图标：编辑 make_icon.py 顶部的配色 / W_PATH，然后
python build/make_icon.py          # → build/windows/icon.ico + icon_preview.png

# 2. 生成 exe 资源
python build/make_syso.py          # → cmd/workbuddy-api/rsrc_windows_amd64.syso

# 3. 构建 + 校验（校验不过就别发布）
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "-H windowsgui" -o dist/workbuddy-api.exe ./cmd/workbuddy-api
python build/check_res.py cmd/workbuddy-api/rsrc_windows_amd64.syso dist/workbuddy-api.exe
```

改了图标后还要同步托盘与弹层 logo：

```bash
python build/make_icon.py          # 顺带刷新 build/logo64.png
cp build/windows/icon.ico cmd/workbuddy-api/build/trayicon.ico
cp build/logo64.png cmd/workbuddy-api/web/logo64.png
```

## 为什么校验是必须的

嵌入图标这条链路上，**任何一步出错都不会让构建失败**：

| 出错点 | 现象 |
|---|---|
| syso 节名不是 `.rsrc` | 构建成功，exe 无图标 |
| `PointerToRawData` 算错 | 构建成功，exe 无图标 |
| 缺符号表 / 重定位表 | 构建成功，exe 无图标（资源目录指针全错） |
| `OffsetToData` 未重定位成 RVA | 构建成功，exe 无图标 |
| `GRPICONDIRENTRY` 里 `nID` 用了 DWORD | 图标异常或不显示 |

`go build` 全程无报错，exe 也能正常启动——只有肉眼在资源管理器里看得到。
所以 `check_res.py` 是**唯一的判据**，构建后必须跑。

`check_res.py` 的关键判据：exe 里每个 `IMAGE_RESOURCE_DATA_ENTRY.OffsetToData`
都必须是落在 `.rsrc` 节范围内的 **RVA**，而不是节内小偏移。后者说明重定位没生效。

## 为什么不用 rsrc / windres

- `akavel/rsrc` 需要 `go install`，本机 GOPROXY 不稳；
- `windres`（binutils）没装。

手写生成器的额外好处：注释里能讲清 PE 资源布局，这正是排查时最需要的知识。

## syso 结构速查

COFF 文件（Go 链接器扫描主包目录下的 `*.syso`，当作目标文件链接进去）：

```
[0]   COFF 头 20B  Machine=0x8664(amd64) sections=1
[20]  节头 40B     Name=".rsrc"  ← 链接器按名字识别，写错会静默忽略
[60]  .rsrc 节数据（节内偏移 = 资源树内部偏移）
[..] 重定位表      每个 OffsetToData DWORD 一条 Type=3(IMAGE_REL_AMD64_ADDR32NB)
[..] 符号表 18B    Name=".rsrc"(内联前 8B) Class=3(STATIC)
[..] 字符串表 4B
```

`.rsrc` 节内是标准 `IMAGE_RESOURCE_DIRECTORY`，**三层目录**：

```
根目录 → type(RT_ICON / RT_GROUP_ICON / RT_VERSION) → id → 叶子 DATA_ENTRY
```

两个易错点：

- **`OffsetToData` 必须是「节内偏移」并配一条重定位**。链接器
  （`cmd/link/internal/ld/pe.go` 的 `addpersrc`）用重定位把它改写成
  `virtualAddress + addend`（addend 从文件里那个 DWORD 读）。
  没有重定位 → 偏移保持节内相对值 → 资源指针全错。
- **`GRPICONDIRENTRY` 是 14 字节**，`nID` 是 **WORD(2B)** 不是 DWORD。
  写 4 字节每条目多 2 字节，整块偏移错位。

权威对照样本：`$GOROOT/src/cmd/link/testdata/pe-binutils/rsrc_amd64.syso`。