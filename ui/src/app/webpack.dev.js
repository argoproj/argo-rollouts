const {merge} = require('webpack-merge');
const common = require('./webpack.common.js');
const BundleAnalyzerPlugin = require('webpack-bundle-analyzer').BundleAnalyzerPlugin;
const webpack = require('webpack');
const dotenv = require('dotenv');
const path = require('path');

// Load environment variables from .env file located two folders up
const env = dotenv.config({ path: path.resolve(__dirname, '../../.env') }).parsed || {};

console.log('env', env);

module.exports = merge(common, {
    mode: 'development',
    plugins: [
        new BundleAnalyzerPlugin(),
        new webpack.DefinePlugin({
            'process.env': JSON.stringify(env), // Pass all .env variables to process.env
        }),
    ],
    devServer: {
        historyApiFallback: {
            disableDotRule: true,
        },
        watchOptions: {
            ignored: [/dist/, /node_modules/],
        },
        headers: {
            'X-Frame-Options': 'SAMEORIGIN',
        },
        host: 'localhost',
        port: 3101,
        proxy: {
            '/api/v1': {
                target: 'http://localhost:3100',
                secure: false,
            },
        },
    },
});
