'use strict;';

const CopyWebpackPlugin = require('copy-webpack-plugin');
const HtmlWebpackPlugin = require('html-webpack-plugin');

const webpack = require('webpack');
const path = require('path');

const config = {
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
        alias: {react: require.resolve('react')},
    },

    module: {
        rules: [
            {
                test: /\.tsx?$/,
                loaders: [`ts-loader?allowTsInNodeModules=true&configFile=${path.resolve('./src/app/tsconfig.json')}`],
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
            SYSTEM_INFO: JSON.stringify({
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
};

module.exports = config;
