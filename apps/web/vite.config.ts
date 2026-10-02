import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import topology from '../../dev/topology.json' with { type: 'json' };

export default defineConfig({
  plugins: [react()],
  server: {
    host: '127.0.0.1',
    port: 5173,
    strictPort: true,
    proxy: Object.fromEntries([...topology.multiprocess, ...topology.single].map(host => [
      `/dev-api/${host.name}`,
      { target: host.admin_url, rewrite: (url: string) => url.replace(`/dev-api/${host.name}`, '') },
    ])),
  },
  define: { __LERNA_DEV_MODE__: JSON.stringify(process.env.LERNA_DEV_MODE ?? 'multiprocess') },
});
