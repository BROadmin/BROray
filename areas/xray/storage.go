package xray

import (
	"fmt"
	mxray "github.com/BROadmin/broray/modules/xray"
)

type storageResources struct{ FlashFree, RAMFree int64 }

func (s *service) storageResources() (storageResources, error) {
	if s.resourceProbe != nil {
		return s.resourceProbe()
	}
	ram, err := mxray.RAMAvailable("/tmp")
	return storageResources{FlashFree: freeKB(s.d.Root.Runtime()) * 1024, RAMFree: ram}, err
}

// The archive is already charged to available RAM at this point. The old
// binary is charged to flash free; a rename does not create another copy.
func checkStorage(r storageResources, binarySize, oldSize int64, lowSpace, backupMade bool) error {
	if binarySize <= 0 || binarySize > mxray.BinaryLimit || oldSize < 0 {
		return jerr("Не удалось подтвердить размер ядра Xray.")
	}
	flashNeed := binarySize + installReserve
	ramNeed := mxray.MemoryReserve
	if lowSpace {
		flashNeed = maxInt(0, binarySize-oldSize) + installReserve
		if !backupMade {
			ramNeed += oldSize
		}
	}
	if r.FlashFree < flashNeed {
		return jerr(fmt.Sprintf("Недостаточно места в /opt: требуется %d байт, доступно %d. Рабочее ядро не изменено.", flashNeed, r.FlashFree))
	}
	if r.RAMFree < ramNeed {
		return jerr(fmt.Sprintf("Недостаточно оперативной памяти: требуется %d байт с резервом, доступно %d.", ramNeed, r.RAMFree))
	}
	return nil
}
