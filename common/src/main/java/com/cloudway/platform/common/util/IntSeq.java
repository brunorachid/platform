/**
 * Cloudway Platform
 * Copyright (c) 2012-2013 Cloudway Technology, Inc.
 * All rights reserved.
 */

package com.cloudway.platform.common.util;

import java.util.OptionalInt;
import java.util.StringJoiner;
import java.util.function.BiFunction;
import java.util.function.IntBinaryOperator;
import java.util.function.IntConsumer;
import java.util.function.IntFunction;
import java.util.function.IntPredicate;
import java.util.function.IntSupplier;
import java.util.function.IntUnaryOperator;
import java.util.function.Predicate;
import java.util.function.Supplier;

/**
 * A sequence of integer values
 */
public interface IntSeq
{
    /**
     * Returns {@code true} if this list contains no elements.
     *
     * @return {@code true} if this list contains no elements
     */
    boolean isEmpty();

    /**
     * Returns the first element in the list.
     *
     * @return the first element in the list
     */
    int head();

    /**
     * Returns remaining elements in the list.
     *
     * @return remaining elements in the list
     */
    IntSeq tail();

    /**
     * Peek the head element as an optional.
     *
     * @return {@code OptionalInt.empty()} if the sequence is empty, otherwise
     * an optional wrapping the head value.
     */
    default OptionalInt peek() {
        return isEmpty() ? OptionalInt.empty() : OptionalInt.of(head());
    }

    // Constructors

    /**
     * Construct an empty list.
     *
     * @return the empty list
     */
    static IntSeq nil() {
        return IntSeqImpl.nil();
    }

    /**
     * Construct a list with head and tail.
     *
     * @param head the first element in the list
     * @param tail the remaining elements in the list
     * @return the list that concatenate from head and tail
     */
    static IntSeq cons(int head, IntSeq tail) {
        return IntSeqImpl.cons(head, tail);
    }

    /**
     * Construct a lazy list with head and a tail generator.
     *
     * @param head the first element in the list
     * @param tail a supplier to generate remaining elements in the list
     * @return the list that concatenate from head and tail
     */
    static IntSeq cons(int head, Supplier<IntSeq> tail) {
        return IntSeqImpl.cons(head, tail);
    }

    /**
     * Construct a list with single element.
     */
    static IntSeq of(int value) {
        return cons(value, nil());
    }

    /**
     * Construct a list with given elements
     */
    static IntSeq of(int... elements) {
        IntSeq res = nil();
        for (int i = elements.length; --i >= 0; ) {
            res = cons(elements[i], res);
        }
        return res;
    }

    /**
     * Wrap an optional as a sequence. The sequence contains one element if optional
     * contains a value, otherwise the sequence is empty if optional is empty.
     */
    static IntSeq wrap(OptionalInt opt) {
        return opt.isPresent() ? of(opt.getAsInt()) : nil();
    }

    /**
     * Returns an infinite list produced by iterative application of
     * a function {@code f} to an initial element {@code seed}, producing
     * list consisting of {@code seed}, {@code f(seed)}, {@code f((f(seed))},
     * etc.
     */
    static IntSeq iterate(int seed, IntUnaryOperator f) {
        return cons(seed, () -> iterate(f.applyAsInt(seed), f));
    }

    /**
     * Returns an infinite list where each element is generated by the provided
     * {@code Supplier}. This is suitable for generating constant sequences,
     * sequences of random elements, etc.
     */
    static IntSeq generate(IntSupplier s) {
        return cons(s.getAsInt(), () -> generate(s));
    }

    /**
     * Create an infinite list where all items are the specified object.
     */
    static IntSeq repeat(int value) {
        return new IntSeq() {
            @Override
            public boolean isEmpty() {
                return false;
            }

            @Override
            public int head() {
                return value;
            }

            @Override
            public IntSeq tail() {
                return this;
            }

            @Override
            public IntSeq reverse() {
                return this;
            }

            @Override
            public String toString() {
                return "[" + value + ", ...]";
            }
        };
    }

    /**
     * Returns a sequence of values from {@code startInclusive} (inclusive)
     * to {@code endExclusive} (exclusive) by an incremental step of {@code 1}.
     *
     * @param startInclusive the (inclusive) initial value
     * @param endExclusive the exclusive upper bound
     */
    static IntSeq range(int startInclusive, int endExclusive) {
        return IntSeqImpl.makeRange(startInclusive, endExclusive - 1, 1);
    }

    /**
     * Returns a sequence of values from {@code startInclusive} (inclusive)
     * to {@code endExclusive} (exclusive) by an incremental step of {@code step}.
     *
     * @param startInclusive the (inclusive) initial value
     * @param endExclusive the exclusive upper bound
     * @param step the incremental step
     * @throws IllegalArgumentException if step if 0
     */
    static IntSeq range(int startInclusive, int endExclusive, int step) {
        return IntSeqImpl.makeRange(startInclusive, endExclusive - 1, step);
    }

    /**
     * Returns a sequence of values from {@code startInclusive} (inclusive)
     * to {@code endInclusive} (inclusive) by an incremental step of {@code 1}.
     *
     * @param startInclusive the (inclusive) initial value
     * @param endInclusive the inclusive upper bound
     */
    static IntSeq rangeClosed(int startInclusive, int endInclusive) {
        return IntSeqImpl.makeRange(startInclusive, endInclusive, 1);
    }

    /**
     * Returns a sequence of values from {@code startInclusive} (inclusive)
     * to {@code endInclusive} (inclusive) by an incremental step of {@code step}.
     *
     * @param startInclusive the (inclusive) initial value
     * @param endInclusive the inclusive upper bound
     * @param step the incremental of range
     * @throws IllegalArgumentException if step if 0
     */
    static IntSeq rangeClosed(int startInclusive, int endInclusive, int step) {
        return IntSeqImpl.makeRange(startInclusive, endInclusive, step);
    }

    /**
     * Returns a infinite sequence of values starting at given number.
     *
     * @param n the initial value
     */
    static IntSeq from(int n) {
        return cons(n, () -> from(n+1));
    }

    /**
     * Returns a {@code Seq} consisting of the elements of this list,
     * each boxed to an {@code Integer}.
     *
     * @return a {@code Seq} consistent of the elements of this list,
     * each boxed to an {@code Integer}
     */
    default Seq<Integer> boxed() {
        return mapToObj(Integer::valueOf);
    }

    // Deconstructions

    /**
     * Returns a predicate that evaluate to true if the list to be tested
     * is empty.
     */
    static Predicate<IntSeq> Nil() {
        return IntSeq::isEmpty;
    }

    /**
     * Returns a conditional case that will be evaluated if the list is empty.
     */
    static <R, X extends Throwable> ConditionCase<IntSeq, R, X>
    Nil(ExceptionSupplier<R, X> supplier) {
        return t -> t.isEmpty() ? supplier : null;
    }

    /**
     * Returns a conditional case that will be evaluated if the list is not
     * empty. The mapper function will accept list head and tail as it's
     * arguments
     */
    static <R, X extends Throwable> ConditionCase<IntSeq, R, X>
    Seq(ExceptionBiFunction<Integer, IntSeq, ? extends R, X> mapper) {
        return s -> s.isEmpty()
            ? null
            : () -> mapper.evaluate(s.head(), IntSeqImpl.delay(s));
    }

    // Operations

    /**
     * Repeat a list infinitely.
     */
    default IntSeq cycle() {
        return isEmpty() ? nil() : IntSeqImpl.cycle(new IntSeq[1], this);
    }

    /**
     * Reverse elements in this list.
     */
    default IntSeq reverse() {
        IntSeq res = nil();
        for (IntSeq xs = this; !xs.isEmpty(); xs = xs.tail()) {
            res = cons(xs.head(), res);
        }
        return res;
    }

    /**
     * Concatenate this list to other list.
     */
    default IntSeq append(IntSeq other) {
        return IntSeqImpl.concat(this, other);
    }

    /**
     * Returns a list consisting of the elements of this list that match
     * the given predicate
     *
     * @param predicate a predicate to apply to each element to determine if it
     * should be included
     * @return the new list
     */
    default IntSeq filter(IntPredicate predicate) {
        for (IntSeq xs = this; !xs.isEmpty(); xs = xs.tail()) {
            if (predicate.test(xs.head())) {
                final IntSeq t = xs;
                return cons(t.head(), () -> t.tail().filter(predicate));
            }
        }
        return nil();
    }

    /**
     * Returns a list consisting of the results of applying the given function
     * to the elements of this list.
     *
     * @param mapper a function to apply to each element
     * @return the new list
     */
    default IntSeq map(IntUnaryOperator mapper) {
        return isEmpty() ? nil() : cons(mapper.applyAsInt(head()), () -> tail().map(mapper));
    }

    /**
     * Returns a list consisting of the results of applying the given function
     * to the elements of this list.
     *
     * @param <R> the element type of the new list
     * @param mapper a function to apply to each element
     * @return the new list
     */
    default <R> Seq<R> mapToObj(IntFunction<? extends R> mapper) {
        return isEmpty() ? Seq.nil() : Seq.cons(mapper.apply(head()), () -> tail().mapToObj(mapper));
    }

    /**
     * Returns a list consisting of the results of replacing each element of
     * this list with the contents of a mapped list produced by applying the
     * provided mapping function to each element.
     *
     * @param mapper a function to apply to each element which produces a list
     * of new values
     * @return the new list
     */
    default IntSeq flatMap(IntFunction<IntSeq> mapper) {
        return isEmpty() ? nil() : IntSeqImpl.concat(mapper.apply(head()), () -> tail().flatMap(mapper));
    }

    /**
     * Returns a list consisting of the results of replacing each element of
     * this list with the contents of a mapped list produced by applying the
     * provided mapping function to each element.
     *
     * @param <R> the element type of the new list
     * @param mapper a function to apply to each element which produces a list
     * of new values
     * @return the new list
     */
    default <R> Seq<R> flatMapToObj(IntFunction<? extends Seq<? extends R>> mapper) {
        return isEmpty() ? Seq.nil() : SeqImpl.concat(mapper.apply(head()), () -> tail().flatMapToObj(mapper));
    }

    /**
     * Performs an action for each element of this list.
     *
     * @param action an action to perform on the elements
     */
    default void forEach(IntConsumer action) {
        forEach(this, action);
    }

    /**
     * Performs an action for each element of given sequence. This static method
     * is provided to enable the sequence node to be garbage collected during
     * iteration for large sequence.
     *
     * @param seq the sequence to iterate
     * @param action an action to perform on the elements
     */
    static void forEach(IntSeq seq, IntConsumer action) {
        for (; !seq.isEmpty(); seq = seq.tail()) {
            action.accept(seq.head());
        }
    }

    /**
     * Zip two lists into one list of integer tuples.
     */
    default Seq<Pair<Integer>> zip(IntSeq other) {
        return zipToObj(other, Pair::make);
    }

    /**
     * Zip two lists into one using a function to produce result values.
     */
    default IntSeq zip(IntSeq other, IntBinaryOperator zipper) {
        return IntSeqImpl.zip(this, other, zipper);
    }

    /**
     * Zip two lists into one using a function to produce result values.
     */
    default <U, R> Seq<R> zipToObj(IntSeq other, BiFunction<Integer, Integer, ? extends R> zipper) {
        return IntSeqImpl.zipToObj(this, other, zipper);
    }

    /**
     * Fold a sequence to the left.
     */
    default int foldLeft(int identity, IntBinaryOperator op) {
        int result = identity;
        for (IntSeq xs = this; !xs.isEmpty(); xs = xs.tail()) {
            result = op.applyAsInt(result, xs.head());
        }
        return result;
    }

    /**
     * Fold a sequence to the right.
     */
    default int foldRight(int identity, IntBinaryOperator op) {
        return reverse().foldLeft(identity, (i,j) -> op.applyAsInt(j, i));
    }

    /**
     * Returns a list with given limited elements taken.
     */
    default IntSeq take(int n) {
        return takeWhile(new IntPredicate() {
            int i;
            @Override
            public boolean test(int t) {
                return i++ < n;
            }
        });
    }

    /**
     * Returns a list with given number of elements dropped.
     */
    default IntSeq drop(int n) {
        return dropWhile(new IntPredicate() {
            int i;
            @Override
            public boolean test(int t) {
                return i++ < n;
            }
        });
    }

    /**
     * Returns a list with all elements skipped for which a predicate evaluates to {@code true}.
     */
    default IntSeq takeWhile(IntPredicate predicate) {
        if (isEmpty() || !predicate.test(head())) {
            return nil();
        } else {
            return cons(head(), () -> tail().takeWhile(predicate));
        }
    }

    /**
     * Returns a list with all elements skipped for which a predicate evaluates to {@code false}.
     */
    default IntSeq takeUntil(IntPredicate predicate) {
        return takeWhile(predicate.negate());
    }

    /**
     * Returns a list with all elements dropped for which a predicate evalutes to {@code true}.
     */
    default IntSeq dropWhile(IntPredicate predicate) {
        for (IntSeq xs = this; !xs.isEmpty(); xs = xs.tail()) {
            if (!predicate.test(xs.head())) {
                return xs;
            }
        }
        return nil();
    }

    /**
     * Returns a list with all elements droped for which a predicate evaluates to {@code false}.
     */
    default IntSeq dropUntil(IntPredicate predicate) {
        return dropWhile(predicate.negate());
    }

    /**
     * Returns the count of elements in this list.
     *
     * @return the count of elements in this list
     */
    default long count() {
        long count = 0;
        for (IntSeq xs = this; !xs.isEmpty(); xs = xs.tail()) {
            count++;
        }
        return count;
    }

    /**
     * Returns whether any elements of this list match the provided
     * predicate. May not evaluate the predicate on all elements if not
     * necessary for determining the result. If the list is empty then
     * {@code false} is returned and the predicate is not evaluated.
     *
     * @param predicate a predicate to apply to elements of this list
     * @return {@code true} if any elements of the list match the provided
     * predicate, other {@code false}
     */
    default boolean anyMatch(IntPredicate predicate) {
        for (IntSeq xs = this; !xs.isEmpty(); xs = xs.tail()) {
            if (predicate.test(xs.head()))
                return true;
        }
        return false;
    }

    /**
     * Returns whether all elements of this list match the provided predicate.
     * May not evaluate the predicate on all elements if not necessary for
     * determining the result. If the list is empty then {@code true} is returned
     * and the predicate is not evaluated.
     *
     * @param predicate a predicate to apply to elements of this list
     * @return {@code true} if either all elements of the list match the
     * provided predicate or the list is empty, otherwise {@code false}
     */
    default boolean allMatch(IntPredicate predicate) {
        for (IntSeq xs = this; !xs.isEmpty(); xs = xs.tail()) {
            if (!predicate.test(xs.head()))
                return false;
        }
        return true;
    }

    /**
     * Returns whether no elements of this list match the provided predicate.
     * May not evaluate the predicate on all elements if not necessary for
     * determining the result. If the list is empty then {@code true} is returned
     * and the predicate is not evaluated.
     *
     * @param predicate a predicate to apply to elements of this list
     * @return {@code true} if either no elements of the stream match the
     * provided predicate or the list is empty, otherwise {@code false}
     */
    default boolean noneMatch(IntPredicate predicate) {
        for (IntSeq xs = this; !xs.isEmpty(); xs = xs.tail()) {
            if (predicate.test(xs.head()))
                return false;
        }
        return true;
    }

    /**
     * Search for an element that satisfy the given predicate.
     *
     * @param predicate the predicate to be tested on element
     * @return {@code OptionalInt.empty()} if element not found in the list, otherwise
     * a {@code OptionalInt} wrapping the found element.
     */
    default OptionalInt find(IntPredicate predicate) {
        for (IntSeq xs = this; !xs.isEmpty(); xs = xs.tail()) {
            int val = xs.head();
            if (predicate.test(val)) {
                return OptionalInt.of(val);
            }
        }
        return OptionalInt.empty();
    }

    /**
     * Concatenate an array of sequences.
     */
    static IntSeq concat(IntSeq... seqs) {
        return concat(seqs, 0, seqs.length);
    }

    /**
     * Concatenate an array of sequences.
     */
    static IntSeq concat(IntSeq[] seqs, int offset, int length) {
        return offset >= length
            ? nil()
            : IntSeqImpl.concat(seqs[offset], () -> concat(seqs, offset+1, length));
    }

    /**
     * Returns the string representation of a sequence.
     */
    default String show() {
        return show(Integer.MAX_VALUE);
    }

    /**
     * Returns the string representation of a sequence.
     *
     * @param n number of elements to be shown
     */
    default String show(int n) {
        return show(n, ", ", "[", "]");
    }

    /**
     * Returns the string representation of a sequence.
     *
     * @param delimiter the sequence of characters to be used between each element
     * @param prefix the sequence of characters to be used at the beginning
     * @param suffix the sequence of characters to be used at the end
     */
    default String show(CharSequence delimiter, CharSequence prefix, CharSequence suffix) {
        return show(Integer.MAX_VALUE, delimiter, prefix, suffix);
    }

    /**
     * Returns the string representation of a sequence.
     *
     * @param n number of elements to be shown
     * @param delimiter the sequence of characters to be used between each element
     * @param prefix the sequence of characters to be used at the beginning
     * @param suffix the sequence of characters to be used at the end
     */
    default String show(int n, CharSequence delimiter, CharSequence prefix, CharSequence suffix) {
        StringJoiner joiner = new StringJoiner(delimiter, prefix, suffix);
        IntSeq xs = this; int i = 0;
        for (; !xs.isEmpty() && i < n; xs = xs.tail(), i++) {
            joiner.add(String.valueOf(xs.head()));
        }
        if (!xs.isEmpty()) {
            joiner.add("...");
        }
        return joiner.toString();
    }
}
