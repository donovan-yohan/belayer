# Steps by Context

Detailed step mappings for each refactor context type. Use these as concrete examples when identifying what work each stage of a strangler fig refactor involves.

> **Note:** These steps are reference examples illustrating the *kind of work* each stage requires. They do not prescribe a 1:1 mapping between steps and goals/PRs. The agent groups these into production-safe goals based on the project's actual needs — see the strangler-fig skill's Phase 3 for goal decomposition guidance.

---

## Backend Service Extraction

### When This Applies
- Extracting a domain concept into its own service class or module
- The target has database persistence (reads/writes its own rows)
- Other services or controllers call into the target
- The extracted concept will own its own data lifecycle

### Example: Extracting SettingsService from a Shop God Object

A `Shop` class handles payment processing, user management, inventory, and shop settings. We extract settings into `SettingsService`.

#### Step 1: Define New Interface

Create `SettingsService` with the extracted responsibilities:

```kotlin
// NEW: src/main/kotlin/com/example/settings/SettingsService.kt
class SettingsService(private val settingsRepository: SettingsRepository) {
    fun getSettings(shopId: String): Settings
    fun updateSettings(shopId: String, update: SettingsUpdate): Settings
    fun resetToDefaults(shopId: String): Settings
}
```

Also create `SettingsRepository` interface and its implementation. The service reads from `shop_settings` table (created in step 3). For now, it can delegate to `Shop` internals as a thin wrapper.

**Rollback**: Delete the new files. No callers redirected yet.

#### Step 2: Redirect Consumers

Update all callers to use `SettingsService` instead of calling `Shop` settings methods directly:

```kotlin
// BEFORE: ShopController.kt
val theme = shop.getTheme(shopId)

// AFTER: ShopController.kt
val theme = settingsService.getSettings(shopId).theme
```

Identify callers via `grep -r "shop\.getTheme\|shop\.updateTheme\|shop\.getSettings"`.

**Rollback**: Revert caller changes. `SettingsService` can still delegate to `Shop` internals.

#### Step 3: Establish New Data Source

Create a dedicated `shop_settings` table:

```sql
-- V20240101__create_shop_settings.sql
CREATE TABLE shop_settings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    shop_id UUID NOT NULL REFERENCES shops(id),
    theme VARCHAR(50) NOT NULL DEFAULT 'default',
    timezone VARCHAR(100) NOT NULL DEFAULT 'UTC',
    currency VARCHAR(10) NOT NULL DEFAULT 'USD',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_shop_settings_shop_id ON shop_settings(shop_id);
```

Apply migration. New table starts empty — existing data still lives in the `shops` table.

**Rollback**: Roll back migration. `SettingsService` still reads from `shops` table.

#### Step 4: Dual Writes

Write settings changes to both `shops` table (old) and `shop_settings` table (new):

```kotlin
fun updateSettings(shopId: String, update: SettingsUpdate): Settings {
    // Write to new source
    val settings = settingsRepository.save(shopId, update)
    // Write to old source (keep in sync during transition)
    shopRepository.updateSettingsColumns(shopId, update)
    return settings
}
```

All writes go to both tables. Reads still come from old table until step 6.

**Rollback**: Remove dual write from `SettingsService`. Reads continue from old table.

#### Step 5: Backfill

Migrate existing settings data from `shops` table to `shop_settings`:

```kotlin
// BackfillShopSettingsJob.kt
fun run() {
    shopRepository.findAllInBatches(batchSize = 500) { shops ->
        shops.forEach { shop ->
            if (!settingsRepository.existsByShopId(shop.id)) {
                settingsRepository.insert(SettingsRecord.fromShop(shop))
            }
        }
    }
}
```

Run as a background job or one-time migration script. Verify row counts match: `SELECT COUNT(*) FROM shops` vs `SELECT COUNT(*) FROM shop_settings`.

**Rollback**: Truncate `shop_settings`. Dual writes from step 4 remain active.

#### Step 6: Switch Reads

Move all reads to `shop_settings`:

```kotlin
fun getSettings(shopId: String): Settings {
    // NOW reads from new table
    return settingsRepository.findByShopId(shopId)
        ?: throw SettingsNotFoundException(shopId)
}
```

Remove the old-table read path. Keep dual writes active during observation period.

**Rollback**: Revert read path back to `shops` table.

#### Step 7: Remove Legacy

After observation confirms new table is authoritative:

1. Remove dual writes from `SettingsService`
2. Remove settings columns from `shops` table (migration)
3. Remove settings methods from `Shop` class
4. Remove any remaining references to old columns

```sql
-- V20240115__remove_settings_from_shops.sql
ALTER TABLE shops DROP COLUMN theme;
ALTER TABLE shops DROP COLUMN timezone;
ALTER TABLE shops DROP COLUMN currency;
```

**Rollback**: Not applicable — this is the point of no return.

---

## Frontend Component Extraction

### When This Applies
- Extracting UI and state from a monolithic component into a focused one
- The extracted component has its own state (Redux slice, React context, local state)
- No server-side database changes required
- Feature flag can gate the new vs. old component

### Example: Extracting UserSettings from a Monolithic Dashboard

A `Dashboard` component renders the entire app: nav, widgets, user settings panel, and notifications. We extract user settings into a dedicated `UserSettings` component.

#### Step 1: Define New Interface

Create `UserSettings` component with extracted props and state:

```tsx
// NEW: src/components/UserSettings/UserSettings.tsx
interface UserSettingsProps {
  userId: string;
}

export const UserSettings: React.FC<UserSettingsProps> = ({ userId }) => {
  const settings = useUserSettings(userId);
  // Renders theme, timezone, notification preferences
};
```

Also create `useUserSettings` hook and `userSettingsSlice` (Redux) or `UserSettingsContext`.

**Rollback**: Delete the new files.

#### Step 2: Redirect Consumers

Replace the inline settings panel in `Dashboard` with the new component:

```tsx
// BEFORE: Dashboard.tsx — inline settings JSX
<div className="settings-panel">
  <ThemeSelector value={userTheme} onChange={setUserTheme} />
  ...
</div>

// AFTER: Dashboard.tsx — delegating to extracted component
<UserSettings userId={currentUser.id} />
```

Gate this behind a feature flag during transition.

**Rollback**: Revert `Dashboard.tsx`. New component still exists but unused.

#### Step 3: Establish New Data Source

**Adapts for frontend**: Instead of a DB table, create a dedicated Redux slice or React context:

```ts
// NEW: src/store/userSettingsSlice.ts
const userSettingsSlice = createSlice({
  name: 'userSettings',
  initialState: { theme: 'light', timezone: 'UTC' },
  reducers: {
    setTheme: (state, action) => { state.theme = action.payload; },
    setTimezone: (state, action) => { state.timezone = action.payload; }
  }
});
```

Register slice in Redux store. Old state still lives in general `userSlice`.

**Rollback**: Remove slice from store.

#### Step 4: Dual Writes

**Adapts for frontend**: Use a feature flag to toggle which state store is the write target, or write to both slices temporarily:

```ts
// Middleware or action creator
function updateTheme(theme: string) {
  return (dispatch: AppDispatch) => {
    dispatch(userSettingsSlice.actions.setTheme(theme)); // new
    dispatch(userSlice.actions.setTheme(theme));         // old (keep in sync)
  };
}
```

**Rollback**: Remove dual dispatch, revert to single dispatch to old slice.

#### Step 5: Backfill

**Adapts for frontend**: Migrate existing state from old slice to new slice on app init:

```ts
// In store setup or app initialization
const existingTheme = store.getState().user.theme;
if (existingTheme && !store.getState().userSettings.theme) {
  store.dispatch(userSettingsSlice.actions.setTheme(existingTheme));
}
```

For persisted state (localStorage, sessionStorage), migrate keys:

```ts
const legacySettings = localStorage.getItem('user_settings');
if (legacySettings) {
  localStorage.setItem('userSettings', legacySettings);
  // Keep old key during observation
}
```

**Rollback**: Remove migration logic.

#### Step 6: Switch Reads

Update `useUserSettings` to read exclusively from new slice:

```ts
// BEFORE: reads from userSlice
const theme = useSelector(state => state.user.theme);

// AFTER: reads from userSettingsSlice
const theme = useSelector(state => state.userSettings.theme);
```

Roll out via feature flag — 10% → 50% → 100%.

**Rollback**: Revert selector, keep both slices active.

#### Step 7: Remove Legacy

After observation at 100% feature flag:

1. Remove settings state from `userSlice`
2. Remove dual writes
3. Remove old selectors
4. Remove feature flag
5. Remove old inline settings JSX from `Dashboard`
6. Clean up legacy localStorage keys

---

## Pure Code Extraction

### When This Applies
- Extracting logic into a new class, module, or utility — no persistent state involved
- The target is a function, validation logic, calculation engine, or transformer
- No database tables, no state stores, no external services
- Only steps 1, 2, and 7 apply

### Example: Extracting a Validation Module from a Controller

An `OrderController` has grown to 800 lines, with 300 lines of inline validation logic mixed with HTTP handling. We extract validation into `OrderValidationService`.

#### Step 1: Define New Interface

Create `OrderValidationService` with all extracted validation methods:

```kotlin
// NEW: src/main/kotlin/com/example/orders/OrderValidationService.kt
class OrderValidationService {
    fun validateCreateOrder(request: CreateOrderRequest): ValidationResult
    fun validateUpdateOrder(orderId: String, request: UpdateOrderRequest): ValidationResult
    fun validateLineItems(items: List<LineItem>): ValidationResult
}
```

Initially, move the validation logic verbatim. Do not change the logic during extraction — that comes after.

**Rollback**: Delete the new file.

#### Step 2: Redirect Consumers

Update `OrderController` (and any other callers) to delegate to `OrderValidationService`:

```kotlin
// BEFORE: OrderController.kt — inline validation
if (request.items.isEmpty()) {
    throw BadRequestException("Order must have at least one item")
}
if (request.items.any { it.quantity <= 0 }) {
    throw BadRequestException("Item quantity must be positive")
}

// AFTER: OrderController.kt — delegated
val validation = orderValidationService.validateCreateOrder(request)
if (!validation.isValid) throw BadRequestException(validation.errorMessage)
```

**Rollback**: Inline the validation back into the controller.

#### Steps 3-6: N/A

No persistent state involved. No database, no state store, no dual writes, no backfill, no read switching.

#### Step 7: Remove Legacy

Remove the old inline validation code from `OrderController`:

1. Delete the inlined validation blocks from controller methods
2. Remove any validation helper methods that were on the controller
3. Run tests to confirm nothing broken
4. If any other classes had similar inline validation, update them too

After step 7, the controller is significantly smaller and `OrderValidationService` owns all order validation logic.
