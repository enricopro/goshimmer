package database

import (
	"io/fs"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"

	"github.com/cockroachdb/errors"
	"github.com/zyedidia/generic/cache"

	"github.com/iotaledger/hive.go/core/byteutils"
	"github.com/iotaledger/hive.go/core/generics/lo"
	"github.com/iotaledger/hive.go/core/generics/options"
	"github.com/iotaledger/hive.go/core/kvstore"

	"github.com/iotaledger/goshimmer/packages/core/epoch"
)

// region Manager //////////////////////////////////////////////////////////////////////////////////////////////////////

type Manager struct {
	permanentStorage kvstore.KVStore
	permanentBaseDir string

	openDBs         *cache.Cache[epoch.Index, *dbInstance]
	bucketedBaseDir string
	openDBsMutex    sync.Mutex

	maxPruned      epoch.Index
	maxPrunedMutex sync.RWMutex

	// The granularity of the DB instances (i.e. how many buckets/epochs are stored in one DB).
	optsGranularity int64
	optsBaseDir     string
	optsDBProvider  DBProvider
	optsMaxOpenDBs  int
}

func NewManager(version Version, opts ...options.Option[Manager]) *Manager {
	m := options.Apply(&Manager{
		maxPruned:       -1,
		optsGranularity: 10,
		optsBaseDir:     "db",
		optsDBProvider:  NewMemDB,
		optsMaxOpenDBs:  10,
	}, opts, func(m *Manager) {
		m.bucketedBaseDir = filepath.Join(m.optsBaseDir, "pruned")
		m.permanentBaseDir = filepath.Join(m.optsBaseDir, "permanent")
		db, err := m.optsDBProvider(m.permanentBaseDir)
		if err != nil {
			panic(err)
		}
		m.permanentStorage = db.NewStore()

		m.openDBs = cache.New[epoch.Index, *dbInstance](m.optsMaxOpenDBs)
		m.openDBs.SetEvictCallback(func(baseIndex epoch.Index, db *dbInstance) {
			db.instance.Close()
		})
	})

	if err := m.checkVersion(version); err != nil {
		panic(err)
	}

	return m
}

// checkVersion checks whether the database is compatible with the current schema version.
// also automatically sets the version if the database is new.
func (m *Manager) checkVersion(version Version) error {
	entry, err := m.permanentStorage.Get(dbVersionKey)
	if errors.Is(err, kvstore.ErrKeyNotFound) {
		// set the version in an empty DB
		return m.permanentStorage.Set(dbVersionKey, lo.PanicOnErr(version.Bytes()))
	}
	if err != nil {
		return err
	}
	if len(entry) == 0 {
		return errors.Errorf("no database version was persisted")
	}
	var storedVersion Version
	if _, err := storedVersion.FromBytes(entry); err != nil {
		return err
	}
	if storedVersion != version {
		return errors.Errorf("incompatible database versions: supported version: %d, version of database: %d", version, storedVersion)
	}
	return nil
}

func (m *Manager) PermanentStorage() kvstore.KVStore {
	return m.permanentStorage
}

func (m *Manager) RestoreFromDisk() (latestBucketIndex epoch.Index) {
	dbInfos := getSortedDBInstancesFromDisk(m.bucketedBaseDir)

	// TODO: what to do if dbInfos is empty? -> start with a fresh DB?

	m.maxPrunedMutex.Lock()
	m.maxPruned = dbInfos[len(dbInfos)-1].baseIndex - 1
	m.maxPrunedMutex.Unlock()

	for _, dbInfo := range dbInfos {
		dbIndex := dbInfo.baseIndex
		var healthy bool
		for bucketIndex := dbIndex + epoch.Index(m.optsGranularity) - 1; bucketIndex >= dbIndex; bucketIndex-- {
			bucket := m.getBucket(bucketIndex)
			healthy = lo.PanicOnErr(bucket.Has(healthKey))
			if healthy {
				return bucketIndex
			}

			m.removeBucket(bucket)
		}
		m.removeDBInstance(dbIndex)
	}

	return 0
}

func (m *Manager) MaxPrunedEpoch() epoch.Index {
	m.maxPrunedMutex.RLock()
	defer m.maxPrunedMutex.RUnlock()

	return m.maxPruned
}

// IsTooOld checks if the Block associated with the given id is too old (in a pruned epoch).
func (m *Manager) IsTooOld(index epoch.Index) (isTooOld bool) {
	m.maxPrunedMutex.RLock()
	defer m.maxPrunedMutex.RUnlock()

	return index <= m.maxPruned
}

func (m *Manager) Get(index epoch.Index, realm kvstore.Realm) kvstore.KVStore {
	if m.IsTooOld(index) {
		return nil
	}

	bucket := m.getBucket(index)
	withRealm, err := bucket.WithRealm(byteutils.ConcatBytes(bucket.Realm(), realm))
	if err != nil {
		panic(err)
	}

	return withRealm
}

func (m *Manager) Flush(index epoch.Index) {
	// Flushing works on DB level
	db := m.getDBInstance(index)
	db.store.Flush()

	// Mark as healthy.
	bucket := m.getBucket(index)
	err := bucket.Set(healthKey, []byte{1})
	if err != nil {
		panic(err)
	}
}

func (m *Manager) PruneUntilEpoch(index epoch.Index) {
	var baseIndexToPrune epoch.Index
	if m.computeDBBaseIndex(index)+epoch.Index(m.optsGranularity)-1 == index {
		// Upper bound of the DB instance should be pruned. So we can delete the entire DB file.
		baseIndexToPrune = index
	} else {
		baseIndexToPrune = m.computeDBBaseIndex(index) - 1
	}

	currentPrunedIndex := m.setMaxPruned(baseIndexToPrune)
	for currentPrunedIndex+epoch.Index(m.optsGranularity) <= baseIndexToPrune {
		currentPrunedIndex = currentPrunedIndex + epoch.Index(m.optsGranularity)
		m.prune(currentPrunedIndex)
	}
}

func (m *Manager) setMaxPruned(index epoch.Index) (previous epoch.Index) {
	m.maxPrunedMutex.Lock()
	defer m.maxPrunedMutex.Unlock()

	if previous = m.maxPruned; previous >= index {
		return
	}

	m.maxPruned = index
	return
}

func (m *Manager) Shutdown() {
	m.openDBsMutex.Lock()
	defer m.openDBsMutex.Unlock()

	m.openDBs.Each(func(index epoch.Index, db *dbInstance) {
		db.instance.Close()
	})

	m.permanentStorage.Close()
}

// getDBInstance returns the DB instance for the given baseIndex or creates a new one if it does not yet exist.
// DBs are created as follows where each db is located in m.basedir/<starting baseIndex>/
// (assuming a bucket granularity=2):
//   baseIndex 0 -> db 0
//   baseIndex 1 -> db 0
//   baseIndex 2 -> db 2
func (m *Manager) getDBInstance(index epoch.Index) (db *dbInstance) {
	m.openDBsMutex.Lock()
	defer m.openDBsMutex.Unlock()

	baseIndex := m.computeDBBaseIndex(index)
	db, exists := m.openDBs.Get(baseIndex)
	if !exists {
		db = m.createDBInstance(baseIndex)
		m.openDBs.Put(baseIndex, db)
	}

	return db
}

// getBucket returns the bucket for the given baseIndex or creates a new one if it does not yet exist.
// A bucket is marked as dirty by default.
// Buckets are created as follows (assuming a bucket granularity=2):
//   baseIndex 0 -> db 0 / bucket 0
//   baseIndex 1 -> db 0 / bucket 1
//   baseIndex 2 -> db 2 / bucket 2
//   baseIndex 3 -> db 2 / bucket 3
func (m *Manager) getBucket(index epoch.Index) (bucket kvstore.KVStore) {
	_, bucket = m.getDBAndBucket(index)
	return bucket
}

func (m *Manager) getDBAndBucket(index epoch.Index) (db *dbInstance, bucket kvstore.KVStore) {
	db = m.getDBInstance(index)
	return db, m.createBucket(db, index)
}

// createDBInstance creates a new DB instance for the given baseIndex.
// If a folder/DB for the given baseIndex already exists, it is opened.
func (m *Manager) createDBInstance(index epoch.Index) (newDBInstance *dbInstance) {
	db, err := m.optsDBProvider(dbPathFromIndex(m.bucketedBaseDir, index))
	if err != nil {
		panic(err)
	}

	return &dbInstance{
		index:    index,
		instance: db,
		store:    db.NewStore(),
	}
}

// createBucket creates a new bucket for the given baseIndex. It uses the baseIndex as a realm on the underlying DB.
func (m *Manager) createBucket(db *dbInstance, index epoch.Index) (bucket kvstore.KVStore) {
	bucket, err := db.store.WithRealm(indexToRealm(index))
	if err != nil {
		panic(err)
	}
	return bucket
}

func (m *Manager) computeDBBaseIndex(index epoch.Index) epoch.Index {
	return index / epoch.Index(m.optsGranularity) * epoch.Index(m.optsGranularity)
}

func (m *Manager) prune(index epoch.Index) {
	dbBaseIndex := m.computeDBBaseIndex(index)
	m.removeDBInstance(dbBaseIndex)
}

func (m *Manager) removeDBInstance(dbBaseIndex epoch.Index) {
	m.openDBsMutex.Lock()
	defer m.openDBsMutex.Unlock()

	m.openDBs.Remove(dbBaseIndex)
	if err := os.RemoveAll(dbPathFromIndex(m.bucketedBaseDir, dbBaseIndex)); err != nil {
		panic(err)
	}
}

func (m *Manager) removeBucket(bucket kvstore.KVStore) {
	err := bucket.Clear()
	if err != nil {
		panic(err)
	}
}

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////

// region Options //////////////////////////////////////////////////////////////////////////////////////////////////////

// WithGranularity sets the granularity of the DB instances (i.e. how many buckets/epochs are stored in one DB).
// It thus also has an impact on how fine-grained buckets/epochs can be pruned.
func WithGranularity(granularity int64) options.Option[Manager] {
	return func(m *Manager) {
		m.optsGranularity = granularity
	}
}

// WithDBProvider sets the DB provider that is used to create new DB instances.
func WithDBProvider(provider DBProvider) options.Option[Manager] {
	return func(m *Manager) {
		m.optsDBProvider = provider
	}
}

// WithBaseDir sets the base directory to store the DB to disk.
func WithBaseDir(baseDir string) options.Option[Manager] {
	return func(m *Manager) {
		m.optsBaseDir = baseDir
	}
}

// WithMaxOpenDBs sets the maximum concurrently open DBs.
func WithMaxOpenDBs(optsMaxOpenDBs int) options.Option[Manager] {
	return func(m *Manager) {
		m.optsMaxOpenDBs = optsMaxOpenDBs
	}
}

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////

// types ///////////////////////////////////////////////////////////////////////////////////////////////////////////////

// DBProvider is a function that creates a new DB instance.
type DBProvider func(dirname string) (DB, error)

type dbInstance struct {
	index    epoch.Index
	instance DB              // actual DB instance on disk within folder index
	store    kvstore.KVStore // KVStore that is used to access the DB instance
}

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////

// utils ///////////////////////////////////////////////////////////////////////////////////////////////////////////////

var healthKey = []byte("bucket_health")

var dbVersionKey = []byte("db_version")

// indexToRealm converts an baseIndex to a realm with some shifting magic.
func indexToRealm(index epoch.Index) kvstore.Realm {
	return []byte{
		byte(0xff & index),
		byte(0xff & (index >> 8)),
		byte(0xff & (index >> 16)),
		byte(0xff & (index >> 24)),
		byte(0xff & (index >> 32)),
		byte(0xff & (index >> 40)),
		byte(0xff & (index >> 48)),
		byte(0xff & (index >> 54)),
	}
}

func dbPathFromIndex(base string, index epoch.Index) string {
	return filepath.Join(base, strconv.FormatInt(int64(index), 10))
}

type dbInstanceFileInfo struct {
	baseIndex epoch.Index
	path      string
}

func getSortedDBInstancesFromDisk(baseDir string) (dbInfos []*dbInstanceFileInfo) {
	files, err := ioutil.ReadDir(baseDir)
	if err != nil {
		panic(err)
	}

	files = lo.Filter(files, func(f fs.FileInfo) bool { return f.IsDir() })
	dbInfos = lo.Map(files, func(f fs.FileInfo) *dbInstanceFileInfo {
		atoi, convErr := strconv.Atoi(f.Name())
		if convErr != nil {
			return nil
		}
		return &dbInstanceFileInfo{
			baseIndex: epoch.Index(atoi),
			path:      filepath.Join(baseDir, f.Name()),
		}
	})
	dbInfos = lo.Filter(dbInfos, func(info *dbInstanceFileInfo) bool { return info != nil })

	sort.Slice(dbInfos, func(i, j int) bool {
		return dbInfos[i].baseIndex > dbInfos[j].baseIndex
	})

	return dbInfos
}

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////
