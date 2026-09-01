/**
 * @file extra_symbol.c
 * @brief 符号审计负向 fixture：导出一个白名单外的多余符号，用于断言 validator 拒绝安装
 */

__attribute__((visibility("default"))) int mock_extra_exported_symbol(void) {
    return 0;
}
