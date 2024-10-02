'use strict;' /* eslint-env node */ /* eslint-disable @typescript-eslint/no-var-requires */;

const CopyWebpackPlugin = require('copy-webpack-plugin');
const HtmlWebpackPlugin = require('html-webpack-plugin');
// const BundleAnalyzerPlugin = require('webpack-bundle-analyzer').BundleAnalyzerPlugin;
const webpack = require('webpack');

const isProd = process.env.NODE_ENV === 'production';

console.log(`Bundling for ${isProd ? 'production' : 'development'}...`);
console.log(__dirname)

const config = {
    mode: isProd ? 'production' : 'development',
    entry: {
        main: './src/app/index.tsx'
    },
    output: {
        filename: '[name].[contenthash].js',
        path: __dirname + '/../../dist/app'
    },

    devtool: isProd ? 'source-map' : 'eval',

    resolve: {
        extensions: ['.ts', '.tsx', '.js', '.json'],
        alias: {react: require.resolve('react')},
        // Polyfills for Node JS for Webpack >= 5
        fallback: {
          "url": require.resolve("url/")
        }
    },

    module: {
        rules: [
            {
                test: /\.tsx?$/,
                loader: 'esbuild-loader'
            },
            // https://github.com/fkhadra/react-toastify/issues/775#issuecomment-1149569290
            {
                test: /\.mjs$/,
                include: /node_modules/,
                type: 'javascript/auto',
            },
            {
                test: /\.scss$/,
                use: ['style-loader', 'css-loader', 'sass-loader']
            },
            {
                test: /\.css$/,
                use: ['style-loader', 'css-loader']
            }
        ]
    },
    stats: 'verbose',
    plugins: [
        new webpack.DefinePlugin({
            'process.env.DEFAULT_TZ': JSON.stringify('UTC'),
            'SYSTEM_INFO': JSON.stringify({
                version: process.env.VERSION || 'latest'
            })
        }),
        new HtmlWebpackPlugin({template: 'src/app/index.html'}),
        new CopyWebpackPlugin({
            patterns: [
              {from: 'src/assets', to: 'assets'},
              {
                  from: 'node_modules/argo-ui/src/assets',
                  to: 'assets',
              },
              {
                  from: 'node_modules/@fortawesome/fontawesome-free/webfonts',
                  to: 'assets/fonts',
              },
            ]
        }),
        // new BundleAnalyzerPlugin()
    ],

    devServer: {
        // this needs to be disabled to allow EventSource to work
        compress: false,
        historyApiFallback: {
            disableDotRule: true
        },
        headers: {
            'X-Frame-Options': 'SAMEORIGIN'
        },
        host: 'localhost',
        port: 3103,
        proxy: [
          {
              context: ['/api/v1'],
              target: 'http://localhost:3100',
              secure: false,
          },
        ]
    }
};

module.exports = config;
