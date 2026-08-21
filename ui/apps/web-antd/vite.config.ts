import { defineConfig } from '@vben/vite-config';

export default defineConfig(async () => {
  return {
    application: {},
    vite: {
      server: {
        proxy: {
          '/api': {
            changeOrigin: true,
            target: 'http://localhost:8000',
            ws: true,
          },
          // 上传文件由后端 StaticFS 提供，dev 下同样代理到 Go 服务
          '/uploads': {
            changeOrigin: true,
            target: 'http://localhost:8000',
          },
        },
      },
    },
  };
});
