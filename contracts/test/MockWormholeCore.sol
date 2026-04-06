// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

contract MockWormholeCore {
    event LogMessagePublished(
        address indexed sender,
        uint64 sequence,
        uint32 nonce,
        bytes payload,
        uint8 consistencyLevel
    );

    address public senderOverride;
    uint64 public nextSequence;

    constructor(address _senderOverride) {
        senderOverride = _senderOverride;
    }

    function publishMessage(bytes calldata payload) external {
        emit LogMessagePublished(
            senderOverride,
            nextSequence,
            0,
            payload,
            1
        );
        nextSequence++;
    }
}
