package generateID
// SnowflakeIDGenerator 雪花算法ID生成器

import (
    "errors"
    "sync"
    "time"
    
)

//TODO
const (
    // 开始时间戳 (2025-01-01 00:00:00 UTC)
    epoch int64 = 1735689600000

    // 机器ID位数
    workerIDBits int64 = 5
    // 数据中心ID位数
    datacenterIDBits int64 = 5
    // 序列号位数
    sequenceBits int64 = 12

    // 最大值
    maxWorkerID      int64 = -1 ^ (-1 << workerIDBits)
    maxDatacenterID  int64 = -1 ^ (-1 << datacenterIDBits)
    sequenceMask     int64 = -1 ^ (-1 << sequenceBits)

    // 左移位数
    workerIDShift      int64 = sequenceBits
    datacenterIDShift  int64 = sequenceBits + workerIDBits
    timestampLeftShift int64 = sequenceBits + workerIDBits + datacenterIDBits
)

// SnowflakeIDGenerator 雪花算法ID生成器
type SnowflakeIDGenerator struct {
    mu            sync.Mutex
    lastTimestamp int64  // 上次生成ID的时间戳
    workerID      int64  // 工作节点ID
    datacenterID  int64  // 数据中心ID
    sequence      int64 // 序列号
}

// NewSnowflakeIDGenerator 初始化雪花算法ID生成器
func NewSnowflakeIDGenerator(workerID, datacenterID int64) (*SnowflakeIDGenerator, error) {

    if workerID < 0 || workerID > maxWorkerID {
        return nil, errors.New("worker ID超出范围")
    }
    if datacenterID < 0 || datacenterID > maxDatacenterID {
        return nil, errors.New("datacenter ID超出范围")
    }
    return &SnowflakeIDGenerator{  
        lastTimestamp: -1,
        workerID:      workerID,
        datacenterID:  datacenterID,
        sequence:      0,
    }, nil
}

// NextID 生成下一个ID
func (s *SnowflakeIDGenerator) NextID() (int64, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    timestamp := time.Now().UnixMilli()

    // 处理时钟回拨 
    if timestamp < s.lastTimestamp {
        // 等待时间追上来
        waitTime := s.lastTimestamp - timestamp
        if waitTime < 100 { // 如果回拨时间较短（小于100毫秒），则等待
            time.Sleep(time.Duration(waitTime) * time.Millisecond)
            timestamp = time.Now().UnixMilli()
            if timestamp < s.lastTimestamp {
                return 0, errors.New("时钟回拨，等待后仍未恢复")
            }
        } else {
            return 0, errors.New("时钟回拨过多,无法生成ID")
        }
    }
    // 同一毫秒内，序列号递增
    if timestamp == s.lastTimestamp {
        s.sequence = (s.sequence + 1) & sequenceMask
        // 同一毫秒内序列号用完
        if s.sequence == 0 {
            // 阻塞到下一毫秒
            for timestamp <= s.lastTimestamp {
                timestamp = time.Now().UnixMilli()
            }
        }
    } else {
        // 不同毫秒，序列号重置
        s.sequence = 0
    }

    s.lastTimestamp = timestamp

    // 组合成ID
    id := ((timestamp - epoch) << timestampLeftShift) |
        (s.datacenterID << datacenterIDShift) |
        (s.workerID << workerIDShift) |
        s.sequence

    return id, nil
}