import { mount } from '@vue/test-utils';
import { defineComponent, h, nextTick } from 'vue';

import { describe, expect, it } from 'vitest';

import ApiComponent from '../api-component.vue';

const TreeSelectStub = defineComponent({
  name: 'TreeSelectStub',
  props: {
    fieldNames: {
      default: undefined,
      type: Object,
    },
    treeData: {
      default: () => [],
      type: Array,
    },
    value: {
      default: undefined,
      type: [Number, String],
    },
  },
  setup() {
    return () => h('div');
  },
});

describe('api-component', () => {
  it('normalizes tree options and keeps the selected value updateable', async () => {
    const wrapper = mount(ApiComponent, {
      attrs: {
        fieldNames: { children: 'children', label: 'label', value: 'value' },
      },
      props: {
        childrenField: 'children',
        component: TreeSelectStub,
        labelField: 'name',
        modelPropName: 'value',
        modelValue: 1,
        options: [
          {
            children: [{ id: 2, name: '研发部' }],
            id: 1,
            name: '总公司',
          },
        ],
        optionsPropName: 'treeData',
        valueField: 'id',
      },
    });

    const treeSelect = wrapper.findComponent(TreeSelectStub);
    const treeData = treeSelect.props('treeData') as Array<{
      children?: Array<{ label: string; value: number }>;
      label: string;
      value: number;
    }>;

    expect(treeSelect.props('fieldNames')).toEqual({
      children: 'children',
      label: 'label',
      value: 'value',
    });
    expect(treeData[0]).toMatchObject({
      children: [{ label: '研发部', value: 2 }],
      label: '总公司',
      value: 1,
    });

    treeSelect.vm.$emit('update:value', 2);
    await nextTick();

    expect(treeSelect.props('value')).toBe(2);
    expect(wrapper.emitted('update:modelValue')).toEqual([[2]]);
  });
});
