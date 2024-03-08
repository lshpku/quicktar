# QuickTar
A random-access archive format with encrytion support.

## Usage

### Archive/Extract

### WebDAV Server

## 文件格式
* 一个QuickTar文件首先可以表示为如下结构体
  ```go
  struct {
    header [32]byte
    data   []byte // 大小必须为32的倍数
    meta   []byte // 大小必须为32的倍数
  }
  ```

* `header`是一个如下的结构体
  ```go
  struct {
    magic   [8]byte  // 必须为"QuickTar"
    metaEnd int64    // meta段结尾的偏移量；
                     // 这个值通常是QuickTar文件的大小，只是为了冗余而记录
    nonce   [16]byte // AES CTR算法的nonce，为系统生成的随机数；
                     // 只在QuickTar创建时生成一次，之后永不修改
  }
  ```

* 关于加密
  * QuickTar文件可以使用AES-CTR加密，加密时`data`和`meta`均会被加密
  * 偏移量为`x`字节的block的IV为`nonce+x/16`，也就是说不用减掉`header`的偏移量
  * QuickTar不记录AES的级别，需要用户在解压时指定

* 关于Checksum
  * 为了简化`meta`设计，QuickTar没有内置Checksum功能
  * 如果用户有Checksum需求，可以以文件形式记录每个文件的Checksum

### Meta
* `meta`的最后是一个32B的结构体
  ```go
  struct {
    size   int64   // meta段的大小
    count  int64   // 包含的文件数量
    random [8]byte // 系统生成的随机数，每次重写meta时重新生成
    zeros  [8]byte // 必须全为0，用于校验密码格式
  }
  ```

* 通过`size`定位到`meta`的开头，首先读出`count`个如下的32B大小的结构体，表示每个文件
  ```go
  struct {
    offset int64 // 文件在整个QuickTar文件中的偏移量
    size   int64
    mode   uint32
    nsec   uint32
    sec    int64
  }
  ```

* 然后紧接着是`count`个文件名，每个文件名后紧跟着一个`'\0'`，所有文件名结束时用0补齐至32对齐

### Data
* `data`段的结尾用0补齐至32B对齐

* 格式说明
  * 普通文件：根据`offset`和`size`读取每个文件即可
  * 文件夹：没有实际数据，其`offset`和`size`均为0
  * 软链接：可以像普通文件一样读，其内容为链接的目的地址
