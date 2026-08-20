module.exports = {
    roots: ['<rootDir>/src'],
    testMatch: ['**/?(*.)+(spec|test).+(ts|tsx|js)'],
    transform: {
        '^.+\\.(ts|tsx)$': 'ts-jest',
    },
    modulePathIgnorePatterns: ['generated'],
    testEnvironment: 'jsdom',
    setupFilesAfterEnv: ['<rootDir>/src/setup-tests.ts'],
    moduleNameMapper: {
        '\\.(css|scss)$': 'identity-obj-proxy',
        '\\.(png|jpg|jpeg|gif|svg|woff|woff2|ttf|eot)$': '<rootDir>/src/file-mock.js',
        // pnpm gives nested packages their own React, which breaks hooks; pin everyone to one copy
        '^react$': '<rootDir>/node_modules/react',
        '^react-dom$': '<rootDir>/node_modules/react-dom',
    },
};
