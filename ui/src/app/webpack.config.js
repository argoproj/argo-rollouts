'use strict;';

const CopyWebpackPlugin = require('copy-webpack-plugin');
const HtmlWebpackPlugin = require('html-webpack-plugin');
const webpack = require('webpack');
const path = require('path');

const isProd = process.env.NODE_ENV === 'production';

console.log(`Starting webpack in ${process.env.NODE_ENV || 'development'} mode`);

const config = {
    mode: isProd ? 'production' : 'development',
    entry: {
        main: './src/app/index.tsx',
    },
    output: {
        filename: '[name].[chunkhash].js',
        path: __dirname + '/../../dist/app',
    },

    devtool: 'source-map',

    resolve: {
        extensions: ['.ts', '.tsx', '.js', '.json', '.ttf'],
    },

    module: {
        rules: [
            {
                test: /\.tsx?$/,
                loaders: [
                    ...(isProd ? [] : ['react-hot-loader/webpack']),
                    `ts-loader?transpileOnly=${!isProd}&allowTsInNodeModules=true&configFile=${path.resolve('./src/app/tsconfig.json')}`,
                ],
            },
            {
                test: /\.scss$/,
                loader: 'style-loader!raw-loader!sass-loader',
            },
            {
                test: /\.css$/,
                loader: 'style-loader!raw-loader',
            },
            {
                test: /\.ttf$/,
                use: ['file-loader'],
            },
        ],
    },
    node: {
        fs: 'empty',
    },
    plugins: [
        new webpack.DefinePlugin({
            'process.env.NODE_ENV': JSON.stringify(process.env.NODE_ENV || 'development'),
            'SYSTEM_INFO': JSON.stringify({
                version: process.env.VERSION || 'latest',
            }),
        }),
        new HtmlWebpackPlugin({template: 'src/app/index.html'}),
        new CopyWebpackPlugin({
            patterns: [
                {from: 'src/assets', to: 'assets'},
                {
                    from: 'node_modules/@fortawesome/fontawesome-free/webfonts',
                    to: 'assets/fonts',
                },
            ],
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
                target: isProd ? '' : 'http://localhost:3100',
                secure: false,
            },
        },
    },
};

module.exports = config;
