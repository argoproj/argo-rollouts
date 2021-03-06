const {merge} = require('webpack-merge');
const common = require('./webpack.common.js');
const BundleAnalyzerPlugin = require('webpack-bundle-analyzer').BundleAnalyzerPlugin;
const webpack = require('webpack');

module.exports = merge(common, {
    mode: 'development',
    plugins: [
        new BundleAnalyzerPlugin(),
        new webpack.DefinePlugin({
            'process.env.NODE_ENV': JSON.stringify('development'),
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
