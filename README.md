# twintest

- 根据`golang`源代码文件，提取函数/方法的逻辑分支，并建立对应的子测试分支
- 方便针对函数/方法的输入与输出构建测试

### 覆盖`if-elseif-else`/`for`/`range`/`switch`/`select`语句
例如
```go
func TestIfelse(s, i int) int {
	if s == 0 {
		return 1
	} else if s == 1 {
		if i == 0 {
			return 2
		} else if i == 1 {
			return 3
		}
	}
	return 0
}
```
将生成
```go
func Test_TestIfelse(t *testing.T) {

	t.Run("if s == 0", func(t *testing.T) { // @37
		t.Run("return 1", func(t *testing.T) { // @38
			t.Skip("TODO")
		})
	})

	t.Run("if s == 1", func(t *testing.T) { // @39
		t.Run("if i == 0", func(t *testing.T) { // @40
			t.Run("return 2", func(t *testing.T) { // @41
				t.Skip("TODO")
			})
		})

		t.Run("if i == 1", func(t *testing.T) { // @42
			t.Run("return 3", func(t *testing.T) { // @43
				t.Skip("TODO")
			})
		})
	})

	t.Run("return 0", func(t *testing.T) { // @46
		t.Skip("TODO")
	})

}
```