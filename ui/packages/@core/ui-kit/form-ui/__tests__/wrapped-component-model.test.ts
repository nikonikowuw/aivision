import { mount } from '@vue/test-utils';
import { defineComponent, h, nextTick } from 'vue';

import { describe, expect, it } from 'vitest';

import { globalShareState } from '@vben-core/shared/global-state';

import { setupVbenForm } from '../src/config';
import { useVbenForm } from '../src/use-vben-form';

type ApiFormComponent = 'ApiTreeSelect';

const TreeSelectStub = defineComponent({
  name: 'TreeSelectStub',
  props: {
    modelValue: {
      default: undefined,
      type: [Number, String],
    },
  },
  emits: ['update:modelValue'],
  setup(_props, { emit }) {
    return () =>
      h('button', {
        onClick: () => emit('update:modelValue', 2),
      });
  },
});

describe('wrapped form components', () => {
  it('forwards ApiTreeSelect selections to the form model', async () => {
    globalShareState.setComponents({ ApiTreeSelect: TreeSelectStub });
    setupVbenForm<ApiFormComponent>({
      config: {
        baseModelPropName: 'value',
        modelPropNameMap: { ApiTreeSelect: 'modelValue' },
      },
    });

    const [Form, formApi] = useVbenForm<{ deptId?: number }, ApiFormComponent>({
      schema: [
        {
          component: 'ApiTreeSelect',
          fieldName: 'deptId',
        },
      ],
      showDefaultActions: false,
    });
    const wrapper = mount(Form);
    await nextTick();

    const treeSelect = wrapper.findComponent(TreeSelectStub);
    await treeSelect.trigger('click');
    await nextTick();

    expect(treeSelect.props('modelValue')).toBe(2);
    expect(await formApi.getValues()).toEqual({ deptId: 2 });
    wrapper.unmount();
  });
});
