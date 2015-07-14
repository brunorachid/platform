;;; Basic macros

(define-macro (define . form)
  (match form
    [((var . formals) . body)
     (if (pair? var)
         `(define ,var (sys:lambda ,formals ,@body))
         `(sys:define ,var (sys:lambda ,formals ,@body)))]
    [(var exp)
     `(sys:define ,var ,exp)]
    [(var)
     `(sys:define ,var ,(void))]
    [else (error 'define "bad syntax" form)]))

(define-macro (lambda . form)
  `(sys:lambda ,@form))

(define-macro (set! . form)
  (match form
    [((proc . args) exp)
     `((setter ,proc) ,@args ,exp)]
    [(var exp)
     `(sys:set! ,var ,exp)]
    [else (error 'set! "bad syntax" form)]))

(define-macro (let . form)
  (match form
    [(tag bindings . body) :when (symbol? tag)
     `((sys:letrec ((,tag (sys:lambda ,(map car bindings) ,@body)))
         ,tag)
       ,@(map (lambda (x) (car (cdr x))) bindings))]
    [(bindings . body)
     `(sys:let ,bindings ,@body)]
    [else (error 'let "bad syntax" form)]))

(define-macro (letrec . form)
  `(sys:letrec ,@form))

(define-macro (let* . form)
  (match form
    [(() . body)
     `(let () ,@body)]
    [(((name init)) . body)
     `(let ((,name ,init)) ,@body)]
    [(((name init) . more) . body)
     `(let ((,name ,init))
       (let* ,more ,@body))]))

(define-macro (letrec* bindings . body)
  `(sys:let ,(map (lambda (x) (list (car x))) bindings)
     ,@(map (lambda (x) `(sys:set! ,@x)) bindings)
     (sys:let () ,@body)))

(define-macro (defmacro . form)
  `(define-macro ,@form))

(define-macro (with-gensyms syms . body)
  `(let ,(map (lambda (s) `(,s (gensym ',s))) syms) ,@body))

(define-macro (cond . form)
  (define (cond/maybe-more test consequent more)
    (if (null? more)
        `(if ,test ,consequent)
        `(if ,test ,consequent (cond ,@more))))

  (match form
    [(('else result . results))
      `(begin ,result ,@results)]
    [(('else))
     (error 'cond "missing expression in 'else' clause" form)]
    [(('else . _) . _)
     (error 'cond "bad syntax ('else' clause must be last)" form)]

    [((test '=> receiver) . more)
     (with-gensyms (t)
       `(let ((,t ,test))
         ,(cond/maybe-more t `(,receiver ,t) more)))]

    [((generator guard '=> receiver) . more)
     (with-gensyms (t)
       `(call-with-values (lambda () ,generator)
         (lambda ,t ,(cond/maybe-more `(apply ,guard ,t)
                                      `(apply ,receiver ,t)
                                      more))))]

    [((test)) test]
    [((test) . more)
     (with-gensyms (t)
       `(let ((,t ,test))
         ,(cond/maybe-more t t more)))]
    [((test . results) . more)
     (cond/maybe-more test `(begin ,@results) more)]

    [else (error 'cond "bad syntax" form)]))

(define-macro (case key . clauses)
  (define (expand-clause atoms result rest)
    (let ((test (if (= 1 (length atoms))
                    `(eqv? ,key ',(car atoms))
                    `(memv ,key ',atoms))))
      (if (null? rest)
          `(if ,test ,result)
          `(if ,test ,result (case ,key ,@rest)))))

  (if (not (symbol? key))
      `(let ((atom-key ,key)) (case atom-key ,@clauses))
      (match clauses
        [(('else '=> receiver))
         `(,receiver ,key)]
        [(('else result . results))
          `(begin ,result ,@results)]
        [(('else))
          (error 'case "missing expression in 'else' clause" clauses)]
        [(('else . _) . _)
          (error 'case "bad syntax ('else' clause must be last" clauses)]
        [((atoms) . _)
          (error 'case "missing expression after datum sequence" (list atoms))]
        [(((atom . atoms) '=> receiver) . rest)
          (expand-clause (cons atom atoms) `(,receiver ,key) rest)]
        [(((atom . atoms) . results) . rest)
          (expand-clause (cons atom atoms) `(begin ,@results) rest)]
        [((atom . results) . rest)
          `(case ,key ((,atom) ,@results) ,@rest)]
        [else (error 'case "bad syntax" clauses)])))

(define-macro (do bindings (test . finish) . body)
  (define (init b)
    (match b
      ((var init . _) :when (symbol? var)
        (list var init))
      (else
        (error 'do "bad syntax" b))))

  (define (step b)
    (match b
      ((var _) var)
      ((_ _ step) step)
      (else (error 'do "bad syntax" b))))

  `(let loop ,(map init bindings)
     (if ,test
         (begin ,@finish)
         (begin ,@body (loop ,@(map step bindings))))))

(define-macro (assert some-cond)
  `(if (not ,some-cond)
     (error 'assert "assertion failure" ',some-cond)))

(define-macro (when pred . actions)
  `(if ,pred (begin ,@actions)))

(define-macro (unless pred . actions)
  `(if (not ,pred) (begin ,@actions)))

(define-macro (while some-cond . some-actions)
  `(do () ((not ,some-cond)) ,@some-actions))

(define-macro (until some-cond . some-actions)
  `(do () (,some-cond) ,@some-actions))

(define-macro (case-lambda . patterns)
  `(lambda args (match args ,@patterns)))

; Java interop

(define-macro (new class . args)
  `(,(jclass class) new: ,@args))

(define-macro (reify class . dispatch)
  `(make-proxy ,(jclass class)
               (list ,@(map (lambda (method)
                              (match method
                                ((name formals . body) :when (symbol? name)
                                  `(cons ',name (lambda ,formals ,@body)))))
                            dispatch))))

(define-macro (do-with obj . actions)
  (with-gensyms (val)
    `(let ((,val ,obj))
      ,@(map (case-lambda
               (((name . args)) :when (keyword? name)
                `(,val ,name ,@args)))
             actions)
      ,val)))

(define-macro (-> obj . calls)
  (match calls
    [() obj]
    [(name) :when (or (symbol? name) (keyword? name))
     `(,obj ',name)]
    [(name . rest) :when (or (symbol? name) (keyword? name))
     `(-> (,obj ',name) ,@rest)]
    [((name . args)) :when (keyword? name)
     `(,obj ,name ,@args)]
    [((name . args) . rest) :when (keyword? name)
     `(-> (,obj ,name ,@args) ,@rest)]
    [else (error '-> "bad syntax" calls)]))


;;; Utilities

(define true  #t)
(define false #f)

(define identity (lambda (x) x))

(define (const . x)
  (if (and (pair? x) (null? (cdr x)))
      (let ((x (car x)))
        (lambda _ x))
      (lambda _ (apply values x))))

(define (negate pred?)
  (lambda args (not (apply pred? args))))

(define (flip proc)
  (lambda (x y) (proc y x)))

(define (compose . fns)
  (let comp ((fns fns))
    (cond ((null? fns) (error 'compose "empty function list"))
          ((null? (cdr fns)) (car fns))
          (else
            (lambda args
              (call-with-values
                (lambda ()
                  (apply (comp (cdr fns)) args))
                (car fns)))))))

(define fold fold-left)
(define reduce fold-right)

(define (unfold func init pred)
  (if (pred init)
      (cons init '())
      (cons init (unfold func (func init) pred))))

(define call/cc call-with-current-continuation)

(define-macro (let/cc k . body)
  `(call/cc (lambda (,k) ,@body)))

(define (object->string obj . maybe-printer)
  (let ((printer (if (pair? maybe-printer)
                     (car maybe-printer)
                     write)))
    (with-output-to-string (lambda () (printer obj)))))

;;; Numerical operations

(define (odd? n)  (= (remainder n 2) 1))
(define (even? n) (= (remainder n 2) 0))

(define (max first . rest)
  (fold (lambda (old new) (if (> old new) old new)) first rest))

(define (min first . rest)
  (fold (lambda (old new) (if (< old new) old new)) first rest))

(define (gcd . vals)
  (define (g a b)
    (if (zero? b) a (g b (remainder a b))))
  (abs (fold g 0 vals)))

(define (lcm . vals)
  (/ (abs (apply * vals)) (apply gcd vals)))

(define (exact-floor x)
  (inexact->exact (floor x)))

(define (exact-ceiling x)
  (inexact->exact (ceiling x)))

(define (exact-truncate x)
  (inexact->exact (truncate x)))

(define (exact-round x)
  (inexact->exact (round x)))

(define (rationalize x y)
  (define (check x)
    (unless (real? x) (error 'rationalize "Invalid type: expected real, found" x)))

  (define (find-between lo hi)
    (if (integer? lo)
        lo
        (let ([lo-int (floor lo)]
              [hi-int (floor hi)])
          (if (< lo-int hi-int)
              (+ 1 lo-int)
              (+ lo-int
                 (/ (find-between (/ (- hi lo-int)) (/ (- lo lo-int)))))))))

  (define (do-find-between lo hi)
    (cond
      [(negative? lo) (- (find-between (- hi) (- lo)))]
      [else (find-between lo hi)]))

  (check x) (check y)
  (let* ([delta (abs y)]
         [lo (- x delta)]
         [hi (+ x delta)])
    (cond
     [(<= lo 0 hi) (if (exact? x) 0 0.0)]
     [(or (inexact? lo) (inexact? hi))
      (exact->inexact (do-find-between (inexact->exact lo) (inexact->exact hi)))]
     [else (do-find-between lo hi)])))

;;; Pairs and lists

(define (caar pair) (car (car pair)))
(define (cadr pair) (car (cdr pair)))
(define (cdar pair) (cdr (car pair)))
(define (cddr pair) (cdr (cdr pair)))
(define (caaar pair) (car (car (car pair))))
(define (caadr pair) (car (car (cdr pair))))
(define (cadar pair) (car (cdr (car pair))))
(define (caddr pair) (car (cdr (cdr pair))))
(define (cdaar pair) (cdr (car (car pair))))
(define (cdadr pair) (cdr (car (cdr pair))))
(define (cddar pair) (cdr (cdr (car pair))))
(define (cdddr pair) (cdr (cdr (cdr pair))))
(define (caaaar pair) (car (car (car (car pair)))))
(define (caaadr pair) (car (car (car (cdr pair)))))
(define (caadar pair) (car (car (cdr (car pair)))))
(define (caaddr pair) (car (car (cdr (cdr pair)))))
(define (cadaar pair) (car (cdr (car (car pair)))))
(define (cadadr pair) (car (cdr (car (cdr pair)))))
(define (caddar pair) (car (cdr (cdr (car pair)))))
(define (cadddr pair) (car (cdr (cdr (cdr pair)))))
(define (cdaaar pair) (cdr (car (car (car pair)))))
(define (cdaadr pair) (cdr (car (car (cdr pair)))))
(define (cdadar pair) (cdr (car (cdr (car pair)))))
(define (cdaddr pair) (cdr (car (cdr (cdr pair)))))
(define (cddaar pair) (cdr (cdr (car (car pair)))))
(define (cddadr pair) (cdr (cdr (car (cdr pair)))))
(define (cdddar pair) (cdr (cdr (cdr (car pair)))))
(define (cddddr pair) (cdr (cdr (cdr (cdr pair)))))

;;; Multiple values

; SRFI 8: Binding to multiple values
(define-macro (receive formals expression . body)
  `(call-with-values (lambda () ,expression)
                     (lambda ,formals ,@body)))

(define-macro (define-values vars exp)
  (if (null? vars)
      `(call-with-values
        (lambda () ,exp)
        (lambda () (void)))
      (let ((tmps (map (lambda (var) (gensym var)) vars)))
        `(begin
          ,@(map (lambda (var) `(define ,var)) vars)
          (call-with-values
            (lambda () ,exp)
            (lambda ,tmps
              ,@(map (lambda (var tmp) `(set! ,var ,tmp))
                     vars tmps)))))))

(define-macro (let-values bindings . body)
  (letrec ((append* (lambda (il l)
                      (if (not (pair? il))
                          (cons il l)
                          (cons (car il)
                                (append* (cdr il) l)))))
           (map* (lambda (proc l)
                   (cond ((null? l) '())
                         ((not (pair? l)) (proc l))
                         (else (cons (proc (car l)) (map* proc (cdr l))))))))
    (let* ([llists (map car bindings)]
           [vars (let loop ((llists llists) (acc '()))
                   (if (null? llists)
                       acc
                       (let* ((llist (car llists))
                              (new-acc
                                (cond ((list? llist) (append llist acc))
                                      ((pair? llist) (append* llist acc))
                                      (else (cons llist acc)))))
                         (loop (cdr llists) new-acc))))]
           [aliases (map (lambda (v) (cons v (gensym v))) vars)]
           [lookup (lambda (v) (cdr (assq v aliases)))]
           [llists2 (let loop ((llists llists) (acc '()))
                      (if (null? llists)
                          (reverse acc)
                          (let* ((llist (car llists))
                                 (new-acc
                                   (cond ((not (pair? llist)) (cons (lookup llist) acc))
                                         (else (cons (map* lookup llist) acc)))))
                            (loop (cdr llists) new-acc))))])
      (let fold ([llists llists]
                 [exps (map (lambda (x) (cadr x)) bindings)]
                 [llists2 llists2])
        (cond ((null? llists)
                `(let ,(map (lambda (v) (list v (lookup v))) vars)
                   ,@body))
              ((and (pair? (car llists2)) (null? (cdar llists2)))
                `(let ((,(caar llists2) ,(car exps)))
                   ,(fold (cdr llists) (cdr exps) (cdr llists2))))
              (else
                `(call-with-values
                   (lambda () ,(car exps))
                   (lambda
                     ,(car llists2)
                     ,(fold (cdr llists) (cdr exps) (cdr llists2))))))))))

(define-macro (let*-values . form)
  (match form
    [(() . body)
     `(let () ,@body)]
    [((binding . more) . body)
     `(let-values (,binding)
       (let*-values ,more ,@body))]))

(define-macro (nth-value n values)
  `(call-with-values
    (lambda () ,values)
    (lambda v (list-ref v ,n))))

;;; Extended Macros

(define-macro (:optional . form)
  (if (and (pair? form)
           (not (symbol? (car form))))
    (with-gensyms (x)
      `(let ((,x ,(car form)))
        (:optional ,x ,@(cdr form))))
    (match form
      ((x default)
        `(if (pair? ,x) (car ,x) ,default))
      ((x default check)
        `(if (pair? ,x)
          (if (not (,check (car ,x)))
              (error ':optional "value is not satisfy: " (car ,x))
              (car ,x))
          ,default))
      (else (error ':optional "bad syntax" form)))))

(define-macro (let-optionals exp bindings . body)
  (define (make-match-clause values defaults body)
    (let ((ids (map car values)))
      `(,ids ,@(flatmap
                 (case-lambda
                   (((var _)) '())
                   (((var _ check))
                     `((if (not ,check)
                           (error 'let-optionals "value is not satisfy" ,var))))
                   (otherwise (error 'let-optionals "bad syntax" otherwise)))
                 values)
        (,body ,@ids ,@(map cadr defaults)))))

  (define (expand processed remaining body result)
    (let ((result (cons (make-match-clause (reverse processed) remaining body) result)))
      (if (null? remaining)
          result
          (expand (cons (car remaining) processed) (cdr remaining) body result))))

  (let ((bproc (gensym)))
    `(let ((,bproc (lambda ,(map car bindings) ,@body)))
      (match ,exp
        ,@(expand '() bindings bproc '())
        (else (error 'let-optionals "too many arguments"))))))

(define-macro (let-optionals* exp bindings . body)
  (define (expand val)
    (match bindings
      [() `(let () ,@body)]

      [((var default))
       `(let ((,var (if (pair? ,val) (car ,val) ,default))) ,@body)]

      [((var default check))
       `(let ((,var (if (pair? ,val) (car ,val) ,default)))
          (if (not ,check)
              (error 'let-optionals* "value is not satisfy: " ,var)
              (begin ,@body)))]

      [((var default) . rest)
        (let ((more (gensym)))
          `(if (pair? ,val)
               (let ((,var (car ,val)) (,more (cdr ,val)))
                 (let-optionals* ,more ,rest ,@body))
               (let ((,var ,default))
                 (let-optionals* () ,rest ,@body))))]

      [((var default check) . rest)
        (let ((more (gensym)))
          `(if (pair? ,val)
             (let ((,var (car ,val)) (,more (cdr ,val)))
               (if (not ,check)
                   (error 'let-optionals* "value is not satisfy: " ,var)
                   (let-optionals* ,more ,rest ,@body)))
             (let ((,var ,default))
               (let-optionals* () ,rest ,@body))))]

      [(tail)
        `(let ((,tail ,val)) ,@body)]

      [else (error 'let-optional* "bad syntax" bindings)]))

  (define (expand-default)
    (match bindings
      [() `(let () ,@body)]
      [((var default . check))
        `(let ((,var ,default)) ,@body)]
      [((var default . check) . rest)
        `(let ((,var ,default)) (let-optionals* () ,rest ,@body))]
      [(tail)
        `(let ((,tail '())) ,@body)]
      [else (error 'let-optional* "bad syntax" bindings)]))

  (cond ((symbol? exp) (expand exp))
        ((null?   exp) (expand-default))
        (else (let ((tmp (gensym))) `(let ((,tmp ,exp)) ,(expand tmp))))))

; SRFI 2: an AND with local bindings, a guarded LET* special form
(define-macro (and-let* claws . body)
  (let* ((new-vars '()) (result (cons 'and '())) (growth-point result))
    (define (andjoin! clause)
      (let ((prev-point growth-point) (clause-cell (cons clause '())))
        (set-cdr! growth-point clause-cell)
        (set! growth-point clause-cell)))

    (if (not (list? claws))
      (error 'and-let* "bindings must be a list " bindings))

    (for-each
      (lambda (claw)
        (match claw
          (var :when (symbol? var)
            (andjoin! var))
          ((expr)
            (andjoin! expr))
          ((var expr) :when (symbol? var)
            (let ((var-cell (cons var '())))
              (if (memq var new-vars)
                (error 'and-let* "duplicate variable " var " in the bindings"))
              (set! new-vars (cons var new-vars))
              (set-cdr! growth-point `((let ((,var ,expr)) (and . ,var-cell))))
              (set! growth-point var-cell)))
          (else
            (error 'and-let* "all ill-formed binding in a syntactic form and-let* " claw))))
      claws)

    (if (not (null? body))
      (andjoin! `(begin ,@body)))
    result))

(define-macro (fluid-let bindings . body)
  (let ((ids (map car bindings))
        (new-tmps (map (lambda (x) (gensym)) bindings))
        (old-tmps (map (lambda (x) (gensym)) bindings)))
    `(let (,@(map list new-tmps (map cadr bindings))
           ,@(map list old-tmps (map void bindings)))
      (dynamic-wind
        (lambda ()
          ,@(map (lambda (ot id) `(set! ,ot ,id)) old-tmps ids)
          ,@(map (lambda (id nt) `(set! ,id ,nt)) ids new-tmps)
          (void))
        (lambda () ,@body)
        (lambda ()
          ,@(map (lambda (id ot) `(set! ,id ,ot)) ids old-tmps)
          (void))))))

; SRFI 9: Defining Record Types

(import-library com.cloudway.fp.scheme.Record)

; We define the following precedures:
;
; (make-record-type <type-name> <field-names>)      -> <record-type>
; (record-constructor <record-type> <field-names>)  -> <constructor>
; (record-predicate <record-type>)                  -> <predicate>
; (record-accessor <record-type> <field-name>)      -> <accessor>
; (record-modifier <record-type> <field-name>)      -> <modifier>
;   where
; (<constructor> <initial-value> ...)           -> <record>
; (<predicate> <value>)                         -> <boolean>
; (<accessor> <record>)                         -> <value>
; (<modifier> <record> <value>)                 -> <unspecified>

; Record types are implemented using vector-like records. The first
; slot of each record contains the record's type, which is itself a
; record.

(define (record-type record)
  (record-ref record 0))

;------------------
; Record types are themselves records, so we first define the type for
; them.  Except for problems with circularities, this could be defined as:
;   (define-record-type :record-type
;     (make-record-type name field-tags)
;     record-type?
;     (name record-type-name)
;     (field-tags record-type-field-tags))
; As it is, we need to define everything by hand.

(define :record-type (make-record 4))
(record-set! :record-type 0 :record-type)   ; Its type is itself
(record-set! :record-type 1 ':record-type)
(record-set! :record-type 2 '(name field-tags))
(record-set! :record-type 3 #f)

; Now that :record-type exists we can define a procedure for making more
; record types.

(define (make-record-type name field-tags)
  (let ((new (make-record 4)))
    (record-set! new 0 :record-type)
    (record-set! new 1 name)
    (record-set! new 2 field-tags)
    (record-set! new 3 #f)
    new))

; Accessors for record types.

(define (record-type-name record-type)
  (record-ref record-type 1))

(define (record-type-field-tags record-type)
  (record-ref record-type 2))

(define (record-type-printer record-type)
  (record-ref record-type 3))

;----------------
; A utility for getting the offset of a field within a record.

(define (%record-field-index type tag)
  (let loop ((i 1) (tags (record-type-field-tags type)))
    (cond ((null? tags)
           (error "record type has no such field" type tag))
          ((eq? tag (car tags))
           i)
          (else
           (loop (+ i 1) (cdr tags))))))

;----------------
; Now we are ready to define RECORD-CONSTRUCTOR and the rest of the
; procedures used by the macro expansion of DEFINE-RECORD-TYPE.

(define (record-constructor type tags)
  (let ((size (length (record-type-field-tags type)))
        (arg-count (length tags))
        (indexes (map (lambda (tag)
                        (%record-field-index type tag))
                      tags)))
    (lambda args
      (if (= (length args) arg-count)
        (let ((new (make-record (+ size 1))))
          (record-set! new 0 type)
          (for-each (lambda (arg i)
                      (record-set! new i arg))
                    args
                    indexes)
          new)
        (error "wrong number of arguments to constructor" type args (length args) arg-count)))))

(define (record-predicate type)
  (lambda (thing)
    (and (record? thing)
         (eq? (record-type thing)
              type))))

(define (record-accessor type tag)
  (let ((index (%record-field-index type tag)))
    (lambda (thing)
      (if (and (record? thing)
               (eq? (record-type thing)
                    type))
        (record-ref thing index)
        (error "accessor applied to bad value" type tag thing)))))

(define (record-modifier type tag)
  (let ((index (%record-field-index type tag)))
    (lambda (thing value)
      (if (and (record? thing)
               (eq? (record-type thing)
                    type))
        (record-set! thing index value)
        (error "modifier applied to bad value" type tag thing)))))

; Definition of DEFINE-RECORD-TYPE
(define-macro (define-record-type type (constructor . constructor-tags) predicate . field-specs)
  `(begin
     (define ,type
       (make-record-type ',type ',(map car field-specs)))
     (define ,constructor
       (record-constructor ,type ',constructor-tags))
     (define ,predicate
       (record-predicate ,type))
     ,@(map (case-lambda
              (((field-tag accessor))
                `(define ,accessor (record-accessor ,type ',field-tag)))
              (((field-tag accessor modifier))
                `(begin
                   (define ,accessor (record-accessor ,type ',field-tag))
                   (define ,modifier (record-modifier ,type ',field-tag)))))
            field-specs)))

(define-macro (define-record-printer (type record out) . body)
  `(if (and (record? ,type) (eq? (record-type ,type) :record-type))
       (record-set! ,type 3 (lambda (,record ,out) ,@body))
       (error 'define-record-printer "not a record type" ,type)))

; SRFI 17: Generalized set!
(define setter
  (let ((setters '()))
    (letrec ((setter
               (lambda (proc)
                 (let ((probe (assv proc setters)))
                   (if probe
                       (cdr probe)
                       (error "No setter for" proc)))))
             (set-setter!
               (lambda (proc setter)
                 (set! setters (cons (cons proc setter) setters)))))
      (set-setter! setter set-setter!)
      setter)))

(define (getter-with-setter get set)
  (let ((proc (lambda args (apply get args))))
    (set! (setter proc) set)
    proc))

(set! (setter car) set-car!)
(set! (setter cdr) set-cdr!)
(set! (setter caar) (lambda (x v) (set-car! (car x) v)))
(set! (setter cadr) (lambda (x v) (set-car! (cdr x) v)))
(set! (setter cdar) (lambda (x v) (set-cdr! (car x) v)))
(set! (setter cddr) (lambda (x v) (set-cdr! (cdr x) v)))
(set! (setter caaar) (lambda (x v) (set-car! (caar x) v)))
(set! (setter caadr) (lambda (x v) (set-car! (cadr x) v)))
(set! (setter cadar) (lambda (x v) (set-car! (cdar x) v)))
(set! (setter caddr) (lambda (x v) (set-car! (cddr x) v)))
(set! (setter cdaar) (lambda (x v) (set-cdr! (caar x) v)))
(set! (setter cdadr) (lambda (x v) (set-cdr! (cadr x) v)))
(set! (setter cddar) (lambda (x v) (set-cdr! (cdar x) v)))
(set! (setter cdddr) (lambda (x v) (set-cdr! (cddr x) v)))
(set! (setter caaaar) (lambda (x v) (set-car! (caaar x) v)))
(set! (setter caaadr) (lambda (x v) (set-car! (caadr x) v)))
(set! (setter caadar) (lambda (x v) (set-car! (cadar x) v)))
(set! (setter caaddr) (lambda (x v) (set-car! (caddr x) v)))
(set! (setter cadaar) (lambda (x v) (set-car! (cdaar x) v)))
(set! (setter cadadr) (lambda (x v) (set-car! (cdadr x) v)))
(set! (setter caddar) (lambda (x v) (set-car! (cddar x) v)))
(set! (setter cadddr) (lambda (x v) (set-car! (cdddr x) v)))
(set! (setter cdaaar) (lambda (x v) (set-cdr! (caaar x) v)))
(set! (setter cdaadr) (lambda (x v) (set-cdr! (caadr x) v)))
(set! (setter cdadar) (lambda (x v) (set-cdr! (cadar x) v)))
(set! (setter cdaddr) (lambda (x v) (set-cdr! (caddr x) v)))
(set! (setter cddaar) (lambda (x v) (set-cdr! (cdaar x) v)))
(set! (setter cddadr) (lambda (x v) (set-cdr! (cdadr x) v)))
(set! (setter cdddar) (lambda (x v) (set-cdr! (cddar x) v)))
(set! (setter cddddr) (lambda (x v) (set-cdr! (cdddr x) v)))
(set! (setter vector-ref) vector-set!)
(set! (setter string-ref) string-set!)
(set! (setter get-field) set-field!)

; SRFI 26: Notation for Specializing Parameters without Currying
(define-macro (cut . form)
  (when (null? form)
    (error 'cut "you need to supply at least a procedure" form))
  (let loop ([xs form] [vars '()] [vals '()] [rest #f])
    (if (null? xs)
        (let ([rvars (reverse vars)]
              [rvals (reverse vals)])
          (if rest
              (let ([rv (gensym)])
                `(lambda (,@rvars . ,rv) (apply ,@rvals ,rv)))
              ;; (begin proc) throws an error if proc is not an expression
              `(lambda ,rvars ((begin ,(car rvals)) ,@(cdr rvals)))))
        (cond ((eq? '<> (car xs))
               (let ((v (gensym)))
                 (loop (cdr xs) (cons v vars) (cons v vals) #f)))
              ((eq? '<...> (car xs))
               (if (null? (cdr xs))
                   (loop '() vars vals #t)
                   (error 'cut "tail pattern after <...> are not supported" from)))
              (else (loop (cdr xs) vars (cons (car xs) vals) #f))))))

(define-macro (cute . form)
  (when (null? form)
    (error 'cute "you need to supply at least a procedure" form))
  (let loop ([xs form] [vars '()] [bs '()] [vals '()] [rest #f])
    (if (null? xs)
        (let ([rvars (reverse vars)]
              [rvals (reverse vals)]
              [rbs   (reverse bs)])
          (if rest
              (let ([rv (gensym)])
                `(let ,rbs (lambda (,@rvars . ,rv) (apply ,@rvals ,rv))))
              `(let ,rbs (lambda ,rvars (,@rvals)))))
        (cond ((eq? '<> (car xs))
               (let ([v (gensym)])
                 (loop (cdr xs) (cons v vars) bs (cons v vals) #f)))
              ((eq? '<...> (car xs))
               (if (null? (cdr xs))
                   (loop '() vars bs vals #t)
                   (error 'cute "tail patterns after <...> are not supported" form)))
              ((pair? (car xs))
                (let ([v (gensym)])
                  (loop (cdr xs)
                        vars
                        (cons (list v (car xs)) bs)
                        (cons v vals)
                        #f)))
              (else
                (loop (cdr xs) vars bs (cons (car xs) vals) #f))))))

; SRFI 31: A special form rec for recursive evaluation
(define-macro (rec . args)
  (match args
    [((name . variables) . body)
      `(letrec ((,name (lambda ,variables . ,@body))) ,name)]
    [(name expression)
      `(letrec ((,name ,expression)) ,name)]
    [else (error 'rec "bad syntax" args)]))

; SRFI 34: Exception handling for programs
(define-macro (guard (var . clauses) . body)
  (define (guard-aux reraise clauses)
    (match clauses
      [(('else . result))
       `(begin ,@result)]

      [((test '=> result))
       (with-gensyms (temp)
         `(let ((,temp ,test))
           (if ,temp
               (,result ,temp)
               ,reraise)))]

      [((test '=> result) . rest)
       (with-gensyms (temp)
         `(let ((,temp ,test))
            (if ,temp
                (,result ,temp)
                ,(guard-aux reraise rest))))]

      [((test)) test]

      [((test) . rest)
       (with-gensyms (temp)
         `(let ((,temp ,test))
            (if ,temp
                ,temp
                ,(guard-aux reraise rest))))]

       [((test . result))
        `(if ,test
             (begin ,@result)
             ,reraise)]

       [((test . result) . rest)
        `(if ,test
             (begin ,@result)
             ,(guard-aux reraise rest))]))

  (with-gensyms (guard-k condition handler-k)
    `((call-with-current-continuation
      (lambda (,guard-k)
        (with-exception-handler
          (lambda (,condition)
            ((call-with-current-continuation
               (lambda (,handler-k)
                 (,guard-k
                   (lambda ()
                     (let ((,var ,condition))
                       ,(guard-aux `(,handler-k (lambda () (raise ,condition)))
                                   clauses))))))))
          (lambda ()
            (call-with-values
              (lambda () ,@body)
              (lambda args
                (,guard-k (lambda () (apply values args))))))))))))

; SRFI 39: Parameter objects
(define (make-parameter init . conv)
  (let ((converter (if (null? conv) (lambda (x) x) (car conv))))
    (let ((global-cell (cons #f (converter init))))
      (letrec ((parameter
                 (lambda new-val
                   (let ((cell (%%sys%dynamic-env-local 'lookup parameter global-cell)))
                     (cond ((null? new-val)
                            (cdr cell))
                           ((null? (cdr new-val))
                            (set-cdr! cell (converter (car new-val))))
                           (else ; this case is needed for parameterize
                            (converter (car new-val))))))))
        (set-car! global-cell parameter)
        parameter))))

(define-macro (define-parameter . form)
  (match form
    [(name value guard)
     `(define ,name (make-parameter ,value ,guard))]
    [(name value)
     `(define ,name (make-parameter ,value))]
    [(name)
     `(define ,name (make-parameter (void)))]))

(define-macro (parameterize bindings . body)
  `(%%sys%dynamic-env-local 'bind
      (list ,@(map car bindings))
      (list ,@(map cadr bindings))
      (lambda () ,@body)))

(define %%sys%dynamic-env-local
  (let ((dynamic-env-local '()))

    (define (bind parameters values body)
      (let* ((old-local dynamic-env-local)
             (new-cells (map (lambda (parameter value)
                               (cons parameter (parameter value #f)))
                             parameters values))
             (new-local (append new-cells old-local)))
        (dynamic-wind
          (lambda () (set! dynamic-env-local new-local))
          body
          (lambda () (set! dynamic-env-local old-local)))))

    (define (lookup parameter global-cell)
      (or (assq parameter dynamic-env-local)
          global-cell))

    (lambda (action . args)
      (cond ((eq? action 'bind) (apply bind args))
            ((eq? action 'lookup) (apply lookup args))
            (else (error "unknown request " action))))))

; SRFI-69 Basic hash tables

(define *default-bound* (- (expt 2 29) 3))

(define (%string-hash s ch-conv bound)
  (let ((hash 31)
        (len (string-length s)))
    (do ((index 0 (+ index 1)))
        ((>= index len) (modulo hash bound))
        (set! hash (modulo (+ (* 37 hash)
                              (char->integer (ch-conv (string-ref s index))))
                           *default-bound*)))))

(define (string-hash s . maybe-bound)
  (%string-hash s (lambda (x) x) (:optional maybe-bound *default-bound*)))

(define (string-ci-hash s . maybe-bound)
  (%string-hash s char-downcase (:optional maybe-bound *default-bound*)))

(define (symbol-hash s . maybe-bound)
  (let ((bound (:optional maybe-bound *default-bound*)))
    (%string-hash (symbol->string s) (lambda (x) x) bound)))

(define (hash obj . maybe-bound)
  (let ((bound (:optional maybe-bound *default-bound*)))
    (cond ((integer? obj) (modulo obj bound))
          ((string? obj) (string-hash obj bound))
          ((symbol? obj) (symbol-hash obj bound))
          ((real? obj) (modulo (+ (numerator obj) (denominator obj)) bound))
          ((number? obj)
            (modulo (+ (hash (real-part obj)) (* 3 (hash (imag-part obj))))
                    bound))
          ((char? obj) (modulo (char->integer obj) bound))
          ((vector? obj) (vector-hash obj bound))
          ((pair? obj) (modulo (+ (hash (car obj)) (* 3 (hash (cdr obj))))
                               bound))
          ((null? obj) 0)
          ((not obj) 0)
          ((procedure? obj) (error "hash: procedures cannot be hashed" obj))
          ((jobject? obj) (obj hashCode:))
          (else 1))))

(define hash-by-identity hash)

(define (vector-hash v bound)
  (let ((hashvalue 571)
        (len (vector-length v)))
    (do ((index 0 (+ index 1)))
        ((>= index len) (modulo hashvalue bound))
        (set! hashvalue (modulo (+ (* 257 hashvalue) (hash (vector-ref v index)))
                                *default-bound*)))))

(define (make-hash-table . args)
  (new java.util.LinkedHashMap))

(define (hash-table? obj)
  (instance-of? obj #!java.util.Map))

(define (%check-hash-table obj)
  (if (not (hash-table? obj))
      (error "type mismatch. required hash-table, found" obj)))

(define (alist->hash-table alist . args)
  (let ((hash-table (apply make-hash-table args)))
    (for-each
      (lambda (elem)
        (hash-table-update!/default
          hash-table (car elem) (lambda (x) x) (cdr elem)))
      alist)
    hash-table))

(define (hash-table-ref hash-table key . maybe-default)
  (%check-hash-table hash-table)
  (cond ((hash-table containsKey: key)
         (hash-table get: key))
        ((null? maybe-default)
         (error 'hash-table-ref "no value associated with" key))
        (else ((car maybe-default)))))

(define (hash-table-ref/default hash-table key default)
  (%check-hash-table hash-table)
  (hash-table getOrDefault: key default))

(define (hash-table-set! hash-table key value)
  (%check-hash-table hash-table)
  (hash-table put: key value)
  (void))

(define (hash-table-delete! hash-table key)
  (%check-hash-table hash-table)
  (hash-table remove: key)
  (void))

(define (hash-table-exists? hash-table key)
  (%check-hash-table hash-table)
  (hash-table contains: key))

(define (hash-table-update! hash-table key function . maybe-default)
  (%check-hash-table hash-table)
  (cond ((hash-table containsKey: key)
         (hash-table put: key (function (hash-table get: key))))
        ((null? maybe-default)
         (error 'hash-table-update! "no value exists for key" key))
        (else (hash-table put: key (function ((car maybe-default)))))))

(define (hash-table-update!/default hash-table key function default)
  (hash-table-update! hash-table key function (lambda () default)))

(define (hash-table-size hash-table)
  (%check-hash-table hash-table)
  (hash-table size:))

(define (hash-table-walk hash-table proc)
  (%check-hash-table hash-table)
  (let ((it ((hash-table entrySet:) iterator:)))
    (do () ((not (it hasNext:)))
      (let ((node (it next:)))
        (proc (node getKey:) (node getValue:))))))

(define (hash-table-fold hash-table f acc)
  (hash-table-walk
    hash-table
    (lambda (key value) (set! acc (f key value acc))))
  acc)

(define (hash-table-copy hash-table)
  (%check-hash-table hash-table)
  (new java.util.LinkedHashMap hash-table))

(define (hash-table-merge! hash-table1 hash-table2)
  (hash-table-walk
    hash-table2
    (lambda (key value) (hash-table-set! hash-table1 key value)))
  hash-table1)

(define (hash-table-clear! hash-table)
  (%check-hash-table hash-table)
  (hash-table clear:))

(define (hash-table->alist hash-table)
  (hash-table-fold
    hash-table
    (lambda (key val acc) (cons (cons key val) acc)) '()))

(define (hash-table-keys hash-table)
  (hash-table-fold hash-table (lambda (key val acc) (cons key acc)) '()))

(define (hash-table-values hash-table)
  (hash-table-fold hash-table (lambda (key val acc) (cons val acc)) '()))


; SRFI 89: Optional positional and named parameters

; Macro expander for define*

(define-macro (define* pattern . body)
  (if (pair? pattern)
      `(define ,(car pattern)
         (lambda* ,(cdr pattern) ,@body))
      `(define ,pattern ,@body)))

; Macro expander for lambda*

(define-macro (lambda* formals . body)

  ;--------------------------------------------------------------------------

  ; Procedures needed at expansion time.

  (define (parse-formals formals)
    (define (variable? x) (symbol? x))

    (define (required-positional? x)
      (variable? x))

    (define (optional-positional? x)
      (and (pair? x)
           (pair? (cdr x))
           (null? (cddr x))
           (variable? (car x))))

    (define (required-named? x)
      (and (pair? x)
           (pair? (cdr x))
           (null? (cddr x))
           (keyword? (car x))
           (variable? (cadr x))))

    (define (optional-named? x)
      (and (pair? x)
           (pair? (cdr x))
           (pair? (cddr x))
           (null? (cdddr x))
           (keyword? (car x))
           (variable? (cadr x))))

    (define (named? x)
      (or (required-named? x)
          (optional-named? x)))

    (define (duplicates? lst)
      (cond ((null? lst)
             #f)
            ((memq (car lst) (cdr lst))
             #t)
            (else
             (duplicates? (cdr lst)))))

    (define (parse-positional-section lst cont)
      (let loop1 ((lst lst) (rev-reqs '()))
        (if (and (pair? lst)
                 (required-positional? (car lst)))
            (loop1 (cdr lst) (cons (car lst) rev-reqs))
            (let loop2 ((lst lst) (rev-opts '()))
              (if (and (pair? lst)
                       (optional-positional? (car lst)))
                  (loop2 (cdr lst) (cons (car lst) rev-opts))
                  (cont lst (cons (reverse rev-reqs) (reverse rev-opts))))))))

    (define (parse-named-section lst cont)
      (let loop ((lst lst) (rev-named '()))
        (if (and (pair? lst)
                 (named? (car lst)))
            (loop (cdr lst) (cons (car lst) rev-named))
            (cont lst (reverse rev-named)))))

    (define (parse-rest lst
                        positional-before-named?
                        positional-reqs/opts
                        named)
      (if (null? lst)
          (parse-end positional-before-named?
                     positional-reqs/opts
                     named
                     #f)
          (if (variable? lst)
              (parse-end positional-before-named?
                         positional-reqs/opts
                         named
                         lst)
              (error 'lambda* "syntax error in formal parameter list" lst))))

    (define (parse-end positional-before-named?
                       positional-reqs/opts
                       named
                       rest)
      (let ((positional-reqs (car positional-reqs/opts))
            (positional-opts (cdr positional-reqs/opts)))
        (let ((vars (append positional-reqs
                            (map car positional-opts)
                            (map cadr named)
                            (if rest (list rest) '())))
              (keys (map car named)))
          (cond ((duplicates? vars)
                 (error 'lambda* "duplicate variable in formal parameter list" vars))
                ((duplicates? keys)
                 (error 'lambda* "duplicate keyword in formal parameter list" keys))
                (else
                 (list positional-before-named?
                       positional-reqs
                       positional-opts
                       named
                       rest))))))

    (define (parse lst)
      (if (and (pair? lst)
               (named? (car lst)))
          (parse-named-section
            lst
            (lambda (lst named)
              (parse-positional-section
                lst
                (lambda (lst positional-reqs/opts)
                  (parse-rest lst
                              #f
                              positional-reqs/opts
                              named)))))
          (parse-positional-section
            lst
            (lambda (lst positional-reqs/opts)
              (parse-named-section
                lst
                (lambda (lst named)
                  (parse-rest lst
                              #t
                              positional-reqs/opts
                              named)))))))

    (parse formals))

  (define (expand-lambda* formals body)
    (define (range lo hi)
      (if (< lo hi)
          (cons lo (range (+ lo 1) hi))
          '()))

    (define (expand positional-before-named?
                    positional-reqs
                    positional-opts
                    named
                    rest)
      (if (and (null? positional-opts) (null? named)) ; direct R5RS equivalent

          `(lambda ,(append positional-reqs (or rest '())) ,@body)

          (with-gensyms ($args $req $opt $key-values)

            (define utility-fns
              `(,@(if (or positional-before-named?
                          (null? positional-reqs))
                      `()
                      `((,$req
                          (lambda ()
                            (if (pair? ,$args)
                                (let ((arg (car ,$args)))
                                  (set! ,$args (cdr ,$args))
                                  arg)
                                (error "too few actual parameters"))))))
                ,@(if (null? positional-opts)
                      `()
                      `((,$opt
                          (lambda (default)
                            (if (pair? ,$args)
                                (let ((arg (car ,$args)))
                                  (set! ,$args (cdr ,$args))
                                  arg)
                                (default))))))))

            (define positional-bindings
              `(,@(if positional-before-named?
                      `()
                      (map (lambda (x)
                             `(,x (,$req)))
                           positional-reqs))
                ,@(map (lambda (x)
                         `(,(car x) (,$opt (lambda () ,(cadr x)))))
                       positional-opts)))

            (define named-bindings
              (if (null? named)
                  `()
                  `((,$key-values
                      (vector ,@(map (lambda (x) `$undefined)
                                     named)))
                    (,$args
                      ($process-keys
                        ,$args
                        ,(alist->hash-table
                          (map (lambda (x i)
                                 (cons (car x) i))
                               named
                               (range 0 (length named))))
                        ,$key-values))
                    ,@(map (lambda (x i)
                             `(,(cadr x)
                               ,(if (null? (cddr x))
                                    `($req-key ,$key-values ,i)
                                    `($opt-key ,$key-values ,i (lambda ()
                                                                 ,(caddr x))))))
                           named
                           (range 0 (length named))))))

            (define rest-binding
              (if (not rest)
                  `((,$args (or (null? ,$args)
                                (error "too many actual parameters"))))
                  `((,rest ,$args))))

            (let ((bindings
                    (append (if positional-before-named?
                                (append utility-fns
                                        positional-bindings
                                        named-bindings)
                                (append named-bindings
                                        utility-fns
                                        positional-bindings))
                            rest-binding)))
              `(lambda ,(append (if positional-before-named?
                                    positional-reqs
                                    '())
                                $args)
                 (let* ,bindings ,@body))))))

    (apply expand (parse-formals formals)))

  (expand-lambda* formals body))

; Handling of named parameters

(define $undefined (list 'undefined))

(define ($req-key key-values i)
  (let ((val (vector-ref key-values i)))
    (if (eq? val $undefined)
        (error "a required named parameter was not provided")
        val)))

(define ($opt-key key-values i default)
  (let ((val (vector-ref key-values i)))
    (if (eq? val $undefined)
        (default)
        val)))

(define ($process-keys args key-hash-table key-values)
  (let loop ((args args))
    (if (null? args)
        args
        (let ((k (car args)))
          (if (not (keyword? k))
              args
              (let ((i (hash-table-ref/default key-hash-table k #f)))
                (if (not i)
                    (error "unknown parameter keyword" k)
                    (if (null? (cdr args))
                        (error "a value was expected after keyword" k)
                        (begin
                          (if (eq? (vector-ref key-values i) $undefined)
                              (vector-set! key-values i (cadr args))
                              (error "duplicate parameter" k))
                          (loop (cddr args)))))))))))


; Generator that based on delimited continuations

(define (yield a)
  (shift k (cons a k)))

(define-macro (generator . body)
  (with-gensyms (susp)
    `(let ((,susp (reset ,@body '())))
      (lambda ()
        (match ,susp
          (() #f)
          ((a . k) (set! ,susp (k '())) a))))))

(define-macro (define-generator name . body)
  `(define ,name (generator ,@body)))


; SRFI 98: An interface to access environment variables.

(define (get-environment-variable name)
  (#!java.lang.System getenv: name))

(define (get-environment-variables)
  (hash-table->alist (#!java.lang.System getenv:)))

(define getenv get-environment-variable)

; SRFI 112: Environment Inquiry

(define (implementation-name) "Scheva")
(define (implementation-version) "1.0")
(define (machine-name) "unknown")
(define (os-type) (#!java.lang.System getProperty: "os.name"))
(define (os-version) (#!java.lang.System getProperty: "os.version"))
(define (cpu-architecture) (#!java.lang.System getProperty: "os.arch"))
