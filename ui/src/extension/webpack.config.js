const path = require("path");

const config = {
  entry: {
    extension: "./src/extension/index.tsx",
  },
  output: {
    filename: "extensions.js",
    path: __dirname + "/dist",
    libraryTarget: "window",
    library: ["extensions", "argoproj.io-Rollout"],
  },
  resolve: {
    extensions: [".ts", ".tsx", ".js", ".json", ".ttf"],
  },
  externals: {
    react: "React", 
  },
  module: {
    rules: [
      {
        test: /\.tsx?$/,
        rules: [
          `ts-loader?allowTsInNodeModules=true&configFile=${path.resolve(
            "./src/extension/tsconfig.json"
          )}`,
        ],
      },
      {
        test: /\.scss$/,
        loader: "style-loader!raw-loader!sass-loader",
      },
      {
        test: /\.css$/,
        loader: "style-loader!raw-loader",
      },
    ],
  },
};

module.exports = config;
