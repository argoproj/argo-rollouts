module.exports = {
    roots: ['<rootDir>/src'],
    testMatch: ['**/?(*.)+(spec|test).+(ts|tsx|js)'],
    transform: {
        '^.+\\.(ts|tsx)$': 'ts-jest',
    },
    moduleNameMapper: {
        '\\.(scss|sass|css)$': 'identity-obj-proxy',
    },
    modulePathIgnorePatterns: ['generated'],
};
