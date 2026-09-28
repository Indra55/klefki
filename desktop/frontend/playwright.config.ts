import {defineConfig} from '@playwright/test';
export default defineConfig({testDir:'./e2e',workers:1,use:{baseURL:'http://127.0.0.1:5177',viewport:{width:1200,height:820}},webServer:{command:'node node_modules/vite/bin/vite.js preview --host 127.0.0.1 --port 5177 --strictPort',url:'http://127.0.0.1:5177',reuseExistingServer:false}});
