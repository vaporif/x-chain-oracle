// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

contract MockChainlinkAggregator {
    int256 public immutable price;
    uint8 public immutable decimals;

    constructor(int256 _price, uint8 _decimals) {
        price = _price;
        decimals = _decimals;
    }

    function latestRoundData()
        external
        view
        returns (
            uint80 roundId,
            int256 answer,
            uint256 startedAt,
            uint256 updatedAt,
            uint80 answeredInRound
        )
    {
        return (1, price, block.timestamp, block.timestamp, 1);
    }
}
